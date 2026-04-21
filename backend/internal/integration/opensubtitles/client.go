package opensubtitles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const defaultBaseURL = "https://api.opensubtitles.com/api/v1"

// Client communicates with the OpenSubtitles.com REST API.
// Search is unauthenticated; Download requires a JWT obtained via login.
// All requests require a valid Api-Key header (register at opensubtitles.com/consumers).
type Client struct {
	baseURL    string
	serverURL  string // base_url from login response, used for download
	apiKey     string
	username   string
	password   string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
}

// NewClient creates an OpenSubtitles client.
func NewClient(apiKey, username, password string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		username:   username,
		password:   password,
		httpClient: httpClient,
	}
}

func (c *Client) authenticate() error {
	payload, err := json.Marshal(map[string]string{
		"username": c.username,
		"password": c.password,
	})
	if err != nil {
		return fmt.Errorf("marshaling login body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/login", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", "MediaGate v1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenSubtitles login returned %d: %s", resp.StatusCode, string(body))
	}

	var loginResp struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return fmt.Errorf("decoding login response: %w", err)
	}
	if loginResp.Token == "" {
		return fmt.Errorf("OpenSubtitles login returned empty token")
	}

	c.token = loginResp.Token
	if loginResp.BaseURL != "" {
		base := strings.TrimRight(loginResp.BaseURL, "/")
		if !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
		c.serverURL = base + "/api/v1"
	}
	return nil
}

// TestConnection validates credentials by performing a login.
func (c *Client) TestConnection() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""     // force re-auth
	c.serverURL = "" // reset server URL
	return c.authenticate()
}

func (c *Client) ensureAuthenticated() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		return c.authenticate()
	}
	return nil
}

// Search queries for subtitles. Does not require authentication.
func (c *Client) Search(params SearchParams) ([]SubtitleResult, error) {
	q := url.Values{}
	if params.TMDbID != nil {
		q.Set("tmdb_id", strconv.Itoa(*params.TMDbID))
	}
	if params.IMDbID != nil {
		q.Set("imdb_id", strconv.Itoa(*params.IMDbID))
	}
	if params.SeasonNumber != nil {
		q.Set("season_number", strconv.Itoa(*params.SeasonNumber))
	}
	if params.EpisodeNumber != nil {
		q.Set("episode_number", strconv.Itoa(*params.EpisodeNumber))
	}
	if params.MovieHash != "" {
		q.Set("moviehash", params.MovieHash)
	}
	if params.Languages != "" {
		q.Set("languages", params.Languages)
	}
	if params.Query != "" {
		q.Set("query", params.Query)
	}

	body, err := c.get("/subtitles", q)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []SubtitleResult `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}
	return resp.Data, nil
}

// Download requests a download link for the given subtitle file.
// Requires authentication (JWT). Uses the server URL returned by login.
func (c *Client) Download(fileID int) (*DownloadResult, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication required for download: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"file_id":    fileID,
		"sub_format": "srt",
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling download body: %w", err)
	}

	// Use server URL from login response (may differ from default base URL).
	base := c.serverURL
	if base == "" {
		base = c.baseURL
	}

	req, err := http.NewRequest(http.MethodPost, base+"/download", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", "MediaGate v1.0")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	// Re-authenticate on 401 (expired token) and retry once.
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		c.mu.Lock()
		c.token = ""
		err := c.authenticate()
		c.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("re-authentication failed: %w", err)
		}
		retryReq, err := http.NewRequest(http.MethodPost, base+"/download", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("creating retry request: %w", err)
		}
		retryReq.Header.Set("Content-Type", "application/json")
		retryReq.Header.Set("Accept", "application/json")
		retryReq.Header.Set("Api-Key", c.apiKey)
		retryReq.Header.Set("User-Agent", "MediaGate v1.0")
		retryReq.Header.Set("Authorization", "Bearer "+c.token)
		resp, err = c.httpClient.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("download retry failed: %w", err)
		}
		defer resp.Body.Close()
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading download response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenSubtitles download returned %d: %s", resp.StatusCode, string(body))
	}

	var result DownloadResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding download response: %w", err)
	}
	return &result, nil
}

// FetchFile downloads the actual subtitle file from a temporary URL.
func (c *Client) FetchFile(downloadURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating fetch request: %w", err)
	}
	req.Header.Set("User-Agent", "MediaGate v1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching subtitle file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subtitle file download returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("reading subtitle file: %w", err)
	}
	return data, nil
}

func (c *Client) get(path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MediaGate v1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenSubtitles API returned %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// FormatLanguages joins a slice of ISO 639-1 codes into a comma-separated string.
func FormatLanguages(langs []string) string {
	return strings.Join(langs, ",")
}
