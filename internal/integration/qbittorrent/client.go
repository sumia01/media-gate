package qbittorrent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrTorrentNotFound = errors.New("torrent not found")
	btihRegexp         = regexp.MustCompile(`(?i)btih:([0-9a-f]{40})`)
)

type Client struct {
	baseURL    string
	username   string
	password   string
	sid        string
	httpClient *http.Client
	mu         sync.Mutex
}

func NewClient(rawURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(rawURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// authenticate performs a login and stores the SID cookie.
func (c *Client) authenticate() error {
	form := url.Values{
		"username": {c.username},
		"password": {c.password},
	}

	resp, err := c.httpClient.Post(c.baseURL+"/api/v2/auth/login", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qBittorrent login returned %d: %s", resp.StatusCode, string(body))
	}

	// qBittorrent returns "Fails." on invalid credentials with 200 status
	if strings.TrimSpace(string(body)) == "Fails." {
		return fmt.Errorf("qBittorrent login failed: invalid credentials")
	}

	// Extract SID from Set-Cookie header
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" {
			c.sid = cookie.Value
			return nil
		}
	}

	return fmt.Errorf("qBittorrent login response missing SID cookie")
}

// TestConnection validates the URL and credentials.
func (c *Client) TestConnection() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.authenticate(); err != nil {
		return err
	}

	// Verify we can actually reach the API
	_, err := c.getUnlocked("/api/v2/app/version")
	return err
}

// ensureAuthenticated acquires the lock and authenticates if needed.
func (c *Client) ensureAuthenticated() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sid == "" {
		return c.authenticate()
	}
	return nil
}

// AddTorrent adds a torrent by magnet link or URL. Returns the info hash if extractable from the magnet URI.
func (c *Client) AddTorrent(downloadURL string, opts AddTorrentOptions) (string, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}

	fields := map[string]string{
		"urls": downloadURL,
	}
	if opts.SavePath != "" {
		fields["savepath"] = opts.SavePath
	}
	if opts.Category != "" {
		fields["category"] = opts.Category
	}
	if opts.Tags != "" {
		fields["tags"] = opts.Tags
	}
	if opts.Paused {
		fields["paused"] = "true"
	}

	body, err := c.postMultipart("/api/v2/torrents/add", fields)
	if err != nil {
		return "", fmt.Errorf("adding torrent: %w", err)
	}

	if strings.TrimSpace(string(body)) == "Fails." {
		return "", fmt.Errorf("qBittorrent rejected the torrent")
	}

	return extractHash(downloadURL), nil
}

// GetTorrent returns info for a specific torrent by hash.
func (c *Client) GetTorrent(hash string) (*TorrentInfo, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	data, err := c.get("/api/v2/torrents/info?hashes=" + url.QueryEscape(hash))
	if err != nil {
		return nil, fmt.Errorf("getting torrent: %w", err)
	}

	var torrents []TorrentInfo
	if err := json.Unmarshal(data, &torrents); err != nil {
		return nil, fmt.Errorf("decoding torrent info: %w", err)
	}

	if len(torrents) == 0 {
		return nil, ErrTorrentNotFound
	}

	return &torrents[0], nil
}

// GetTorrents returns info for all torrents.
func (c *Client) GetTorrents() ([]TorrentInfo, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	data, err := c.get("/api/v2/torrents/info")
	if err != nil {
		return nil, fmt.Errorf("listing torrents: %w", err)
	}

	var torrents []TorrentInfo
	if err := json.Unmarshal(data, &torrents); err != nil {
		return nil, fmt.Errorf("decoding torrents: %w", err)
	}

	return torrents, nil
}

// get performs an authenticated GET request. Retries once on 403 (expired session).
func (c *Client) get(path string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getUnlocked(path)
}

// getUnlocked performs a GET without acquiring the mutex (caller must hold it or manage locking).
func (c *Client) getUnlocked(path string) ([]byte, error) {
	data, statusCode, err := c.doRequest(http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusForbidden {
		c.sid = ""
		if err := c.authenticate(); err != nil {
			return nil, fmt.Errorf("re-authentication failed: %w", err)
		}
		data, statusCode, err = c.doRequest(http.MethodGet, path, nil, "")
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("qBittorrent API returned %d: %s", statusCode, string(data))
	}

	return data, nil
}

// postForm performs an authenticated POST with form-encoded body. Retries once on 403.
func (c *Client) postForm(path string, values url.Values) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, statusCode, err := c.doRequest(http.MethodPost, path, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusForbidden {
		c.sid = ""
		if err := c.authenticate(); err != nil {
			return nil, fmt.Errorf("re-authentication failed: %w", err)
		}
		data, statusCode, err = c.doRequest(http.MethodPost, path, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded")
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("qBittorrent API returned %d: %s", statusCode, string(data))
	}

	return data, nil
}

// postMultipart performs an authenticated multipart POST. Retries once on 403.
func (c *Client) postMultipart(path string, fields map[string]string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	send := func() ([]byte, int, error) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for k, v := range fields {
			if err := w.WriteField(k, v); err != nil {
				return nil, 0, fmt.Errorf("writing multipart field %q: %w", k, err)
			}
		}
		if err := w.Close(); err != nil {
			return nil, 0, fmt.Errorf("closing multipart writer: %w", err)
		}
		return c.doRequest(http.MethodPost, path, &buf, w.FormDataContentType())
	}

	data, statusCode, err := send()
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusForbidden {
		c.sid = ""
		if err := c.authenticate(); err != nil {
			return nil, fmt.Errorf("re-authentication failed: %w", err)
		}
		data, statusCode, err = send()
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("qBittorrent API returned %d: %s", statusCode, string(data))
	}

	return data, nil
}

// doRequest executes an HTTP request with the SID cookie set.
func (c *Client) doRequest(method, path string, body io.Reader, contentType string) ([]byte, int, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.sid != "" {
		req.AddCookie(&http.Cookie{Name: "SID", Value: c.sid})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("reading response: %w", err)
	}

	return data, resp.StatusCode, nil
}

// extractHash attempts to extract the info hash from a magnet URI.
func extractHash(magnetURL string) string {
	matches := btihRegexp.FindStringSubmatch(magnetURL)
	if len(matches) >= 2 {
		return strings.ToLower(matches[1])
	}
	return ""
}

// AddTorrentOptions configures optional parameters for adding a torrent.
type AddTorrentOptions struct {
	SavePath string
	Category string
	Tags     string
	Paused   bool
}

// TorrentInfo represents the status of a torrent in qBittorrent.
type TorrentInfo struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	State         string  `json:"state"`
	Progress      float64 `json:"progress"`
	Ratio         float64 `json:"ratio"`
	SeedingTime   int     `json:"seeding_time"`
	SavePath      string  `json:"save_path"`
	Size          int64   `json:"size"`
	DownloadSpeed int64   `json:"dlspeed"`
	UploadSpeed   int64   `json:"upspeed"`
}

// MapState maps a qBittorrent torrent state to a simplified category.
func MapState(qbitState string) string {
	switch qbitState {
	case "downloading", "metaDL", "allocating", "forcedDL", "stalledDL",
		"queuedDL", "checkingDL", "checkingResumeData":
		return "downloading"
	case "uploading", "forcedUP", "stalledUP", "queuedUP", "checkingUP":
		return "seeding"
	case "pausedUP":
		return "completed"
	case "pausedDL":
		return "paused"
	case "moving":
		return "moving"
	case "error", "missingFiles", "unknown":
		return "error"
	default:
		return "unknown"
	}
}
