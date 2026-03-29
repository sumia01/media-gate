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
	"time"
)

const defaultBaseURL = "https://api4.thetvdb.com/v4"

type Client struct {
	baseURL    string
	apiKey     string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
}

func NewClient(apiKey string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
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

func (c *Client) SearchSeries(query string, year *int) ([]SeriesResult, error) {
	c.mu.Lock()
	if c.token == "" {
		if err := c.authenticate(); err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	c.mu.Unlock()

	params := url.Values{"query": {query}}
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
	c.mu.Lock()
	if c.token == "" {
		if err := c.authenticate(); err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	c.mu.Unlock()

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
	c.mu.Lock()
	if c.token == "" {
		if err := c.authenticate(); err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	c.mu.Unlock()

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
	req.Header.Set("Authorization", "Bearer "+c.token)

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
		return nil, fmt.Errorf("TVDB API returned %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// Types

type SeriesResult struct {
	ID           int    `json:"tvdb_id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	FirstAirDate string `json:"first_air_time"`
	ImageURL     string `json:"image_url"`
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
