package cardigann

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SearchResult holds a single torrent result from an indexer search.
type SearchResult struct {
	Title                string
	Details              string
	Download             string
	Size                 string
	Seeders              int
	Leechers             int
	Date                 int64
	Category             string
	CategoryDesc         string
	ImdbID               string
	DownloadVolumeFactor float64
	UploadVolumeFactor   float64
	Description          string
}

// Engine executes a Cardigann definition: login, search, and result parsing.
type Engine struct {
	def        *Definition
	config     map[string]string
	httpClient *http.Client
	baseURL    string
	loggedIn   bool
}

// NewEngine creates a Cardigann engine for the given definition and user config.
func NewEngine(def *Definition, config map[string]string) (*Engine, error) {
	if len(def.Links) == 0 {
		return nil, fmt.Errorf("definition %q has no links", def.ID)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}

	return &Engine{
		def:    def,
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
		baseURL: strings.TrimRight(def.Links[0], "/"),
	}, nil
}

// TestConnection attempts to log in to the indexer.
func (e *Engine) TestConnection(ctx context.Context) error {
	return e.Login(ctx)
}

// Login authenticates with the indexer.
func (e *Engine) Login(ctx context.Context) error {
	if e.def.Login.Path == "" {
		e.loggedIn = true
		return nil
	}

	tmplCtx := &TemplateContext{Config: e.config}

	rendered, err := RenderInputs(e.def.Login.Inputs, tmplCtx)
	if err != nil {
		return fmt.Errorf("rendering login inputs: %w", err)
	}

	form := url.Values{}
	for k, v := range rendered {
		form.Set(k, v)
	}

	loginURL := e.resolveURL(e.def.Login.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("parsing login response: %w", err)
	}

	for _, errBlock := range e.def.Login.Error {
		sel := doc.Find(errBlock.Selector)
		if sel.Length() > 0 {
			msg := strings.TrimSpace(sel.Text())
			if msg == "" {
				msg = "login error detected"
			}
			return fmt.Errorf("login failed: %s", msg)
		}
	}

	if err := e.verifyLogin(ctx); err != nil {
		return err
	}

	e.loggedIn = true
	slog.Debug("indexer login successful", "indexer", e.def.ID)
	return nil
}

func (e *Engine) verifyLogin(ctx context.Context) error {
	if e.def.Login.Test.Path == "" {
		return nil
	}

	testURL := e.resolveURL(e.def.Login.Test.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return fmt.Errorf("creating login test request: %w", err)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login test request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("reading login test response: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("parsing login test response: %w", err)
	}

	if e.def.Login.Test.Selector != "" {
		if doc.Find(e.def.Login.Test.Selector).Length() == 0 {
			return fmt.Errorf("login verification failed: selector %q not found", e.def.Login.Test.Selector)
		}
	}
	return nil
}

// Search queries the indexer and returns parsed results.
func (e *Engine) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	if !e.loggedIn {
		if err := e.Login(ctx); err != nil {
			return nil, fmt.Errorf("auto-login: %w", err)
		}
	}

	categories := e.resolveCategories(query)

	tmplCtx := &TemplateContext{
		Config:     e.config,
		Keywords:   query.Q,
		Query:      query,
		Categories: categories,
	}

	searchPath := e.def.Search.Paths[0].Path
	rendered, err := RenderInputs(e.def.Search.Inputs, tmplCtx)
	if err != nil {
		return nil, fmt.Errorf("rendering search inputs: %w", err)
	}

	searchURL := e.resolveURL(searchPath)
	params := url.Values{}
	rawSuffix := ""

	for k, v := range rendered {
		if k == "$raw" {
			rawSuffix = v
			continue
		}
		params.Set(k, v)
	}

	fullURL := searchURL + "?" + params.Encode() + rawSuffix

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("reading search response: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parsing search response: %w", err)
	}

	return e.parseRows(doc, tmplCtx)
}

// Caps returns the definition's capabilities.
func (e *Engine) Caps() *Caps {
	return &e.def.Caps
}

func (e *Engine) parseRows(doc *goquery.Document, tmplCtx *TemplateContext) ([]SearchResult, error) {
	rowSelector := e.def.Search.Rows.Selector
	if rowSelector == "" {
		return nil, fmt.Errorf("no row selector defined")
	}

	var results []SearchResult
	doc.Find(rowSelector).Each(func(_ int, row *goquery.Selection) {
		result, err := e.parseRow(row, tmplCtx)
		if err != nil {
			slog.Debug("skipping row", "error", err, "indexer", e.def.ID)
			return
		}
		results = append(results, *result)
	})

	return results, nil
}

func (e *Engine) parseRow(row *goquery.Selection, tmplCtx *TemplateContext) (*SearchResult, error) {
	fields := make(map[string]string)

	// First pass: extract all fields so .Result references work.
	for name, fieldDef := range e.def.Search.Fields {
		val, err := e.extractField(row, fieldDef)
		if err != nil {
			if fieldDef.Optional {
				continue
			}
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields[name] = val
	}

	// Second pass: render any text fields that reference .Result
	for name, fieldDef := range e.def.Search.Fields {
		if fieldDef.Text == "" {
			continue
		}
		ctx := &TemplateContext{
			Config:     tmplCtx.Config,
			Keywords:   tmplCtx.Keywords,
			Query:      tmplCtx.Query,
			Categories: tmplCtx.Categories,
			Result:     fields,
		}
		rendered, err := RenderTemplate(fieldDef.Text, ctx)
		if err != nil {
			if fieldDef.Optional {
				continue
			}
			return nil, fmt.Errorf("rendering text for field %q: %w", name, err)
		}
		fields[name] = rendered
	}

	r := &SearchResult{
		Title:       fields["title"],
		Details:     e.maybeResolveURL(fields["details"]),
		Download:    e.maybeResolveURL(fields["download"]),
		Size:        fields["size"],
		ImdbID:      extractImdbID(fields["imdbid"]),
		Description: fields["description"],
	}

	r.Category, r.CategoryDesc = e.mapCategory(fields["category"])
	r.Seeders, _ = strconv.Atoi(strings.TrimSpace(fields["seeders"]))
	r.Leechers, _ = strconv.Atoi(strings.TrimSpace(fields["leechers"]))
	r.DownloadVolumeFactor = parseFloat(fields["downloadvolumefactor"], 1.0)
	r.UploadVolumeFactor = parseFloat(fields["uploadvolumefactor"], 1.0)

	if dateStr := fields["date"]; dateStr != "" {
		if ts, err := strconv.ParseInt(dateStr, 10, 64); err == nil {
			r.Date = ts
		}
	}

	return r, nil
}

func (e *Engine) extractField(row *goquery.Selection, fieldDef FieldDef) (string, error) {
	// Case mapping: select the first matching selector from the case map.
	if len(fieldDef.Case) > 0 && fieldDef.Selector == "" && fieldDef.Text == "" {
		for selector, value := range fieldDef.Case {
			if row.Find(selector).Length() > 0 {
				val := value
				if len(fieldDef.Filters) > 0 {
					var err error
					val, err = ApplyFilters(val, fieldDef.Filters)
					if err != nil {
						return "", err
					}
				}
				return val, nil
			}
		}
		return "", nil
	}

	// Text-only field (static value or template, processed later).
	if fieldDef.Text != "" && fieldDef.Selector == "" {
		return fieldDef.Text, nil
	}

	sel := row
	if fieldDef.Selector != "" {
		sel = row.Find(fieldDef.Selector)
		if sel.Length() == 0 {
			if fieldDef.Optional {
				return "", nil
			}
			return "", fmt.Errorf("selector %q not found", fieldDef.Selector)
		}
	}

	var value string
	if fieldDef.Attribute != "" {
		value, _ = sel.Attr(fieldDef.Attribute)
	} else {
		value = sel.Text()
	}
	value = strings.TrimSpace(value)

	if len(fieldDef.Filters) > 0 {
		var err error
		value, err = ApplyFilters(value, fieldDef.Filters)
		if err != nil {
			return "", fmt.Errorf("applying filters: %w", err)
		}
	}

	return value, nil
}

func (e *Engine) resolveURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return e.baseURL + "/" + strings.TrimLeft(path, "/")
}

func (e *Engine) maybeResolveURL(val string) string {
	if val == "" {
		return ""
	}
	if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
		return val
	}
	if strings.HasPrefix(val, "/") || strings.Contains(val, ".php") {
		return e.resolveURL(val)
	}
	return val
}

func (e *Engine) mapCategory(siteCatID string) (string, string) {
	for _, cm := range e.def.Caps.CategoryMappings {
		if cm.ID == siteCatID {
			return cm.Cat, cm.Desc
		}
	}
	return "", ""
}

func (e *Engine) resolveCategories(query SearchQuery) []string {
	if len(query.Categories) > 0 {
		return query.Categories
	}

	// Map search type to appropriate site categories.
	var cats []string
	searchType := query.Type
	for _, cm := range e.def.Caps.CategoryMappings {
		cat := cm.Cat
		switch searchType {
		case "movie-search":
			if strings.HasPrefix(cat, "Movies") {
				cats = append(cats, cm.ID)
			}
		case "tv-search":
			if strings.HasPrefix(cat, "TV") {
				cats = append(cats, cm.ID)
			}
		case "music-search":
			if strings.HasPrefix(cat, "Audio") {
				cats = append(cats, cm.ID)
			}
		case "book-search":
			if strings.HasPrefix(cat, "Books") {
				cats = append(cats, cm.ID)
			}
		}
	}
	return cats
}

func extractImdbID(s string) string {
	if s == "" {
		return ""
	}
	// Extract ttNNNNNNN pattern from URL or raw string.
	idx := strings.Index(s, "tt")
	if idx == -1 {
		return ""
	}
	end := idx + 2
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == idx+2 {
		return ""
	}
	return s[idx:end]
}

func parseFloat(s string, defaultVal float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return defaultVal
	}
	return f
}
