package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://api.themoviedb.org/3"

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
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

func (c *Client) TestConnection() error {
	_, err := c.get("/configuration")
	return err
}

func (c *Client) SearchMovie(query string, year *int) ([]MovieResult, error) {
	params := url.Values{"query": {query}}
	if year != nil {
		params.Set("year", strconv.Itoa(*year))
	}
	body, err := c.getWithParams("/search/movie", params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []MovieResult `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.Results, nil
}

func (c *Client) SearchTV(query string, year *int) ([]TVResult, error) {
	params := url.Values{"query": {query}}
	if year != nil {
		params.Set("first_air_date_year", strconv.Itoa(*year))
	}
	body, err := c.getWithParams("/search/tv", params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []TVResult `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.Results, nil
}

func (c *Client) GetMovie(id int) (*MovieDetails, error) {
	body, err := c.getWithParams(fmt.Sprintf("/movie/%d", id), url.Values{"append_to_response": {"credits"}})
	if err != nil {
		return nil, err
	}
	var details MovieDetails
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &details, nil
}

func (c *Client) GetTV(id int) (*TVDetails, error) {
	body, err := c.getWithParams(fmt.Sprintf("/tv/%d", id), url.Values{"append_to_response": {"credits,external_ids"}})
	if err != nil {
		return nil, err
	}
	var details TVDetails
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &details, nil
}

func (c *Client) GetTVSeason(seriesID, seasonNumber int) (*TVSeasonDetails, error) {
	body, err := c.get(fmt.Sprintf("/tv/%d/season/%d", seriesID, seasonNumber))
	if err != nil {
		return nil, err
	}
	var details TVSeasonDetails
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &details, nil
}

func (c *Client) get(path string) ([]byte, error) {
	return c.getWithParams(path, nil)
}

func (c *Client) getWithParams(path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}

	q := u.Query()
	q.Set("api_key", c.apiKey)
	for k, vs := range params {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB API returned %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// Types

type MovieResult struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
}

type TVResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	FirstAirDate string `json:"first_air_date"`
	PosterPath   string `json:"poster_path"`
}

type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CastMember struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
}

type CrewMember struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	ProfilePath string `json:"profile_path"`
}

type Credits struct {
	Cast []CastMember `json:"cast"`
	Crew []CrewMember `json:"crew"`
}

type MovieDetails struct {
	MovieResult
	ImdbID  string   `json:"imdb_id"`
	Genres  []Genre  `json:"genres"`
	Runtime int      `json:"runtime"`
	Status  string   `json:"status"`
	Credits *Credits `json:"credits,omitempty"`
}

type ExternalIds struct {
	ImdbID string `json:"imdb_id"`
	TvdbID int    `json:"tvdb_id"`
}

type TVDetails struct {
	TVResult
	Genres          []Genre      `json:"genres"`
	NumberOfSeasons int          `json:"number_of_seasons"`
	Status          string       `json:"status"`
	Credits         *Credits     `json:"credits,omitempty"`
	ExternalIds     *ExternalIds `json:"external_ids,omitempty"`
}

type TVEpisode struct {
	ID            int    `json:"id"`
	EpisodeNumber int    `json:"episode_number"`
	SeasonNumber  int    `json:"season_number"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
	AirDate       string `json:"air_date"`
	Runtime       int    `json:"runtime"`
}

type TVSeasonDetails struct {
	ID           int         `json:"id"`
	SeasonNumber int         `json:"season_number"`
	Episodes     []TVEpisode `json:"episodes"`
}
