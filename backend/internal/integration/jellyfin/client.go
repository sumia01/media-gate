package jellyfin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/sumia01/media-gate/internal/integration/mediaserver"
)

// Client communicates with Jellyfin using an administrator-created API key.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: httpClient}
}

// TestConnection exercises the authenticated library-management endpoint.
func (c *Client) TestConnection() error {
	_, err := c.ListLibraries()
	return err
}

func (c *Client) ListLibraries() ([]mediaserver.Section, error) {
	body, err := c.request(http.MethodGet, "/Library/VirtualFolders")
	if err != nil {
		return nil, err
	}
	var folders []struct {
		ItemID         string   `json:"ItemId"`
		Name           string   `json:"Name"`
		CollectionType string   `json:"CollectionType"`
		Locations      []string `json:"Locations"`
	}
	if err := json.Unmarshal(body, &folders); err != nil {
		return nil, errors.New("invalid Jellyfin library response")
	}
	libraries := make([]mediaserver.Section, 0, len(folders))
	for _, folder := range folders {
		var mediaType string
		switch folder.CollectionType {
		case "movies":
			mediaType = "movie"
		case "tvshows":
			mediaType = "show"
		default:
			continue
		}
		if _, err := uuid.Parse(folder.ItemID); err != nil {
			continue
		}
		locations := append([]string{}, folder.Locations...)
		libraries = append(libraries, mediaserver.Section{ID: folder.ItemID, Title: folder.Name, Type: mediaType, Locations: locations})
	}
	return libraries, nil
}

func (c *Client) RefreshLibrary(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return errors.New("invalid Jellyfin library ID")
	}
	// Recursive is needed by older Jellyfin releases; current releases recurse
	// automatically. Default refresh modes discover changes without replacing metadata.
	_, err = c.request(http.MethodPost, "/Items/"+parsed.String()+"/Refresh?recursive=true&metadataRefreshMode=Default&imageRefreshMode=Default&replaceAllMetadata=false&replaceAllImages=false")
	return err
}

func (c *Client) request(method, path string) ([]byte, error) {
	req, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return nil, errors.New("invalid Jellyfin request URL")
	}
	// Jellyfin can disable legacy headers such as X-Emby-Token.
	req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", c.apiKey))
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Transport errors can contain URLs/credentials; never expose them.
		return nil, errors.New("Jellyfin request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Jellyfin returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, errors.New("reading Jellyfin response failed")
	}
	return body, nil
}
