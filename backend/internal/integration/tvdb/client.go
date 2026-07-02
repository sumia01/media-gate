package tvdb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
)

const defaultBaseURL = "https://api4.thetvdb.com/v4"

type Client struct {
	baseURL    string
	apiKey     string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
}

func NewClient(apiKey string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

func (c *Client) authenticate() error {
	payload, err := json.Marshal(map[string]string{"apikey": c.apiKey})
	if err != nil {
		return fmt.Errorf("marshaling login body: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/login", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TVDB login returned %d: %s", resp.StatusCode, string(body))
	}

	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return fmt.Errorf("decoding login response: %w", err)
	}

	c.token = loginResp.Data.Token
	return nil
}

func (c *Client) TestConnection() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authenticate()
}

// ensureAuthenticated acquires the lock, authenticates if needed, and unlocks.
func (c *Client) ensureAuthenticated() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		return c.authenticate()
	}
	return nil
}

func (c *Client) SearchSeries(query string, year *int) ([]SeriesResult, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, err
	}

	params := url.Values{"query": {query}, "type": {"series"}}
	if year != nil {
		params.Set("year", strconv.Itoa(*year))
	}

	body, err := c.get("/search", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []SeriesResult `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) GetSeries(id int) (*SeriesDetails, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, err
	}

	body, err := c.get(fmt.Sprintf("/series/%d/extended", id), nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data SeriesDetails `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) GetSeriesEpisodes(seriesID, seasonNumber int) ([]EpisodeEntry, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, err
	}

	params := url.Values{"season": {strconv.Itoa(seasonNumber)}}
	body, err := c.get(fmt.Sprintf("/series/%d/episodes/default", seriesID), params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Episodes []EpisodeEntry `json:"episodes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.Data.Episodes, nil
}

// get performs a GET request and, if the token has expired (401), transparently
// re-authenticates once and retries the request before giving up.
func (c *Client) get(path string, params url.Values) ([]byte, error) {
	body, status, err := c.doGet(path, params)
	if err != nil {
		return nil, err
	}

	if status == http.StatusUnauthorized {
		if reauthErr := c.reauthenticate(); reauthErr != nil {
			return nil, fmt.Errorf("TVDB API returned 401 and re-authentication failed: %w", reauthErr)
		}
		body, status, err = c.doGet(path, params)
		if err != nil {
			return nil, err
		}
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("TVDB API returned %d: %s", status, string(body))
	}

	return body, nil
}

// doGet performs a single GET request against path using the current token
// and returns the raw body and status code. Non-200 responses are not
// treated as errors here so get() can inspect the status code (e.g. to
// detect an expired token) before deciding how to react.
func (c *Client) doGet(path string, params url.Values) ([]byte, int, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing URL: %w", err)
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.getToken())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("reading response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// getToken returns the currently cached token. Guarded by mu since
// reauthenticate may replace it concurrently.
func (c *Client) getToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// reauthenticate clears the cached token and fetches a fresh one. It takes
// mu itself (mirroring TestConnection/ensureAuthenticated) and must never be
// called while the caller already holds the lock, to avoid a deadlock.
func (c *Client) reauthenticate() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	return c.authenticate()
}

// Types

type SeriesResult struct {
	TVDBID       string `json:"tvdb_id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	FirstAirDate string `json:"first_air_time"`
	ImageURL     string `json:"image_url"`
}

// ID returns the TVDB ID as an int.
func (r SeriesResult) ID() int {
	n, _ := strconv.Atoi(r.TVDBID)
	return n
}

type Character struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	PeopleID     int    `json:"peopleId"`
	PersonName   string `json:"personName"`
	PeopleType   string `json:"peopleType"`
	PersonImgURL string `json:"personImgURL"`
	Sort         int    `json:"sort"`
}

type SeasonEntry struct {
	ID     int `json:"id"`
	Number int `json:"number"`
}

type RemoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

type SeriesDetails struct {
	ID         int           `json:"id"`
	Name       string        `json:"name"`
	Overview   string        `json:"overview"`
	FirstAired string        `json:"firstAired"`
	Image      string        `json:"image"`
	Seasons    []SeasonEntry `json:"seasons"`
	Characters []Character   `json:"characters"`
	Status     Status        `json:"status"`
	RemoteIds  []RemoteID    `json:"remoteIds"`
}

// ImdbID extracts the IMDb ID from the RemoteIds list, if present.
func (d *SeriesDetails) ImdbID() string {
	for _, r := range d.RemoteIds {
		if r.SourceName == "IMDB" {
			return r.ID
		}
	}
	return ""
}

// MaxSeasonNumber returns the highest season number from the Seasons list,
// ignoring season 0 (specials) and any duplicates caused by alternative
// orderings (absolute, DVD, etc.) that TVDB may include.
func (d *SeriesDetails) MaxSeasonNumber() int {
	max := 0
	for _, s := range d.Seasons {
		if s.Number > max {
			max = s.Number
		}
	}
	return max
}

type Status struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type EpisodeEntry struct {
	ID           int    `json:"id"`
	Number       int    `json:"number"`
	SeasonNumber int    `json:"seasonNumber"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Aired        string `json:"aired"`
	Runtime      int    `json:"runtime"`
}
