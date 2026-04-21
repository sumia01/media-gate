package qbittorrent

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

var (
	ErrTorrentNotFound = errors.New("torrent not found")
)

type Client struct {
	baseURL    string
	username   string
	password   string
	sid        string
	httpClient *http.Client
	mu         sync.Mutex
}

func NewClient(rawURL, username, password string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(rawURL, "/"),
		username:   username,
		password:   password,
		httpClient: httpClient,
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

// EnsureCategory creates a category in qBittorrent if it doesn't already exist.
func (c *Client) EnsureCategory(name string) error {
	if name == "" {
		return nil
	}
	if err := c.ensureAuthenticated(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	_, err := c.postForm("/api/v2/torrents/createCategory", url.Values{
		"category": {name},
		"savePath": {""},
	})
	// qBit returns 409 if category already exists — that's fine
	if err != nil && !strings.Contains(err.Error(), "409") {
		return err
	}
	return nil
}

// AddTorrentFile uploads a .torrent file to qBittorrent. Returns the info hash computed from the torrent data.
func (c *Client) AddTorrentFile(fileName string, data []byte, opts AddTorrentOptions) (string, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}

	hash, err := InfoHash(data)
	if err != nil {
		return "", fmt.Errorf("computing info hash: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	send := func() ([]byte, int, error) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, err := w.CreateFormFile("torrents", fileName)
		if err != nil {
			return nil, 0, fmt.Errorf("creating form file: %w", err)
		}
		if _, err := part.Write(data); err != nil {
			return nil, 0, fmt.Errorf("writing torrent data: %w", err)
		}
		if opts.SavePath != "" {
			_ = w.WriteField("savepath", opts.SavePath)
			_ = w.WriteField("downloadPath", opts.SavePath) // qBit 4.4+
			_ = w.WriteField("useAutoTMM", "false")
		}
		if opts.Category != "" {
			_ = w.WriteField("category", opts.Category)
		}
		if opts.Tags != "" {
			_ = w.WriteField("tags", opts.Tags)
		}
		if opts.Paused {
			_ = w.WriteField("paused", "true")
		}
		if err := w.Close(); err != nil {
			return nil, 0, fmt.Errorf("closing multipart writer: %w", err)
		}
		return c.doRequest(http.MethodPost, "/api/v2/torrents/add", &buf, w.FormDataContentType())
	}

	respData, statusCode, err := send()
	if err != nil {
		return "", err
	}

	if statusCode == http.StatusForbidden {
		c.sid = ""
		if err := c.authenticate(); err != nil {
			return "", fmt.Errorf("re-authentication failed: %w", err)
		}
		respData, statusCode, err = send()
		if err != nil {
			return "", err
		}
	}

	if statusCode != http.StatusOK {
		return "", fmt.Errorf("qBittorrent API returned %d: %s", statusCode, string(respData))
	}

	if strings.TrimSpace(string(respData)) == "Fails." {
		return "", fmt.Errorf("qBittorrent rejected the torrent file")
	}

	return hash, nil
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

// TorrentFile represents a single file within a torrent.
type TorrentFile struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// GetTorrentFiles returns the files inside a torrent.
func (c *Client) GetTorrentFiles(hash string) ([]TorrentFile, error) {
	if hash == "" {
		return nil, nil
	}
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	data, err := c.get("/api/v2/torrents/files?hash=" + url.QueryEscape(hash))
	if err != nil {
		return nil, fmt.Errorf("getting torrent files: %w", err)
	}

	var files []TorrentFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("decoding torrent files: %w", err)
	}

	return files, nil
}

// DeleteTorrent removes a torrent from qBittorrent.
func (c *Client) DeleteTorrent(hash string, deleteFiles bool) error {
	if hash == "" {
		return nil
	}
	if err := c.ensureAuthenticated(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	delFiles := "false"
	if deleteFiles {
		delFiles = "true"
	}
	_, err := c.postForm("/api/v2/torrents/delete", url.Values{
		"hashes":      {hash},
		"deleteFiles": {delFiles},
	})
	return err
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

// InfoHash computes the SHA1 info hash from raw .torrent file bytes.
func InfoHash(data []byte) (string, error) {
	// Find the "info" key in the top-level bencode dict and hash its raw value.
	// Bencode format: d...4:info<value>...e
	infoKey := []byte("4:info")
	idx := bytes.Index(data, infoKey)
	if idx == -1 {
		return "", fmt.Errorf("info key not found in torrent data")
	}

	start := idx + len(infoKey)
	end, err := bencodeValueEnd(data, start)
	if err != nil {
		return "", fmt.Errorf("parsing info value: %w", err)
	}

	h := sha1.Sum(data[start:end])
	return fmt.Sprintf("%x", h), nil
}

// bencodeValueEnd returns the index one past the end of the bencode value starting at pos.
func bencodeValueEnd(data []byte, pos int) (int, error) {
	if pos >= len(data) {
		return 0, fmt.Errorf("unexpected end of data")
	}

	switch {
	case data[pos] == 'i': // integer: i<digits>e
		end := bytes.IndexByte(data[pos:], 'e')
		if end == -1 {
			return 0, fmt.Errorf("unterminated integer")
		}
		return pos + end + 1, nil

	case data[pos] >= '0' && data[pos] <= '9': // string: <len>:<data>
		colonIdx := bytes.IndexByte(data[pos:], ':')
		if colonIdx == -1 {
			return 0, fmt.Errorf("invalid string encoding")
		}
		length := 0
		for _, b := range data[pos : pos+colonIdx] {
			length = length*10 + int(b-'0')
		}
		return pos + colonIdx + 1 + length, nil

	case data[pos] == 'l': // list: l<items>e
		p := pos + 1
		for p < len(data) && data[p] != 'e' {
			next, err := bencodeValueEnd(data, p)
			if err != nil {
				return 0, err
			}
			p = next
		}
		if p >= len(data) {
			return 0, fmt.Errorf("unterminated list")
		}
		return p + 1, nil

	case data[pos] == 'd': // dict: d<key><value>...e
		p := pos + 1
		for p < len(data) && data[p] != 'e' {
			// key (string)
			next, err := bencodeValueEnd(data, p)
			if err != nil {
				return 0, err
			}
			p = next
			// value
			next, err = bencodeValueEnd(data, p)
			if err != nil {
				return 0, err
			}
			p = next
		}
		if p >= len(data) {
			return 0, fmt.Errorf("unterminated dict")
		}
		return p + 1, nil

	default:
		return 0, fmt.Errorf("unknown bencode type at pos %d: %c", pos, data[pos])
	}
}
