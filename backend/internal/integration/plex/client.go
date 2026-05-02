package plex

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Section represents a Plex library section.
type Section struct {
	ID        string
	Title     string
	Type      string // "movie" or "show"
	Locations []string
}

// Client communicates with a Plex Media Server.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a Plex API client.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

// TestConnection verifies the server is reachable and the token is valid.
func (c *Client) TestConnection() error {
	_, err := c.doGet("/")
	return err
}

// ListSections returns all library sections from the Plex server.
func (c *Client) ListSections() ([]Section, error) {
	body, err := c.doGet("/library/sections")
	if err != nil {
		return nil, err
	}

	var container struct {
		XMLName xml.Name `xml:"MediaContainer"`
		Dirs    []struct {
			Key       string `xml:"key,attr"`
			Title     string `xml:"title,attr"`
			Type      string `xml:"type,attr"`
			Locations []struct {
				Path string `xml:"path,attr"`
			} `xml:"Location"`
		} `xml:"Directory"`
	}
	if err := xml.Unmarshal(body, &container); err != nil {
		return nil, fmt.Errorf("parsing sections XML: %w", err)
	}

	sections := make([]Section, 0, len(container.Dirs))
	for _, d := range container.Dirs {
		s := Section{
			ID:    d.Key,
			Title: d.Title,
			Type:  normalizeSectionType(d.Type),
		}
		for _, loc := range d.Locations {
			s.Locations = append(s.Locations, loc.Path)
		}
		sections = append(sections, s)
	}
	return sections, nil
}

// RefreshSection triggers a library scan on a specific section.
func (c *Client) RefreshSection(sectionID string) error {
	_, err := c.doGet("/library/sections/" + sectionID + "/refresh")
	return err
}

// doGet performs an authenticated GET request and returns the response body.
func (c *Client) doGet(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "application/xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("plex returned status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// normalizeSectionType maps Plex's type strings to our canonical types.
func normalizeSectionType(plexType string) string {
	switch plexType {
	case "movie":
		return "movie"
	case "show":
		return "show"
	default:
		return plexType
	}
}
