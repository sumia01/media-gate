package cardigann

import (
	"bytes"
	"context"
	"encoding/json"
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
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
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
}

// Engine executes a Cardigann definition: login, search, and result parsing.
type Engine struct {
	def        *Definition
	config     map[string]string
	httpClient *http.Client
	baseURL    string
	loggedIn   bool
}

// defaultUserAgent mimics a standard browser to avoid being blocked by
// Cloudflare or similar bot-detection on public indexers.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

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
			Timeout:   30 * time.Second,
			Jar:       jar,
			Transport: &uaTransport{base: http.DefaultTransport},
		},
		baseURL: strings.TrimRight(def.Links[0], "/"),
	}, nil
}

// uaTransport injects a browser User-Agent header into every outgoing request.
type uaTransport struct {
	base http.RoundTripper
}

func (t *uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", defaultUserAgent)
	}
	return t.base.RoundTrip(req)
}

// TestConnection attempts to log in to the indexer.
func (e *Engine) TestConnection(ctx context.Context) error {
	return e.Login(ctx)
}

// Login authenticates with the indexer.
func (e *Engine) Login(ctx context.Context) error {
	if e.def.Login.Method == "cookie" {
		return e.loginCookie(ctx)
	}

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

	// Pre-GET: many trackers (e.g. nCore) set a session cookie on the login
	// page that must be present on the subsequent POST, otherwise login fails.
	preReq, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return fmt.Errorf("creating login pre-request: %w", err)
	}
	preResp, err := e.doRequest(preReq)
	if err != nil {
		return fmt.Errorf("login pre-request: %w", err)
	}
	preResp.Body.Close()

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

	body, err := e.readBody(resp.Body)
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

// loginCookie handles cookie-based authentication by injecting the user-provided
// cookie string into the HTTP client's cookie jar and verifying the session.
func (e *Engine) loginCookie(ctx context.Context) error {
	tmplCtx := &TemplateContext{Config: e.config}

	rendered, err := RenderInputs(e.def.Login.Inputs, tmplCtx)
	if err != nil {
		return fmt.Errorf("rendering cookie login inputs: %w", err)
	}

	cookieStr := rendered["cookie"]
	if cookieStr == "" {
		return fmt.Errorf("cookie login: no cookie value provided")
	}

	baseURL, err := url.Parse(e.baseURL)
	if err != nil {
		return fmt.Errorf("parsing base URL: %w", err)
	}

	// Parse the cookie string (format: "name=value; name2=value2; ...")
	// and inject into the jar.
	var cookies []*http.Cookie
	for _, part := range strings.Split(cookieStr, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eqIdx := strings.IndexByte(part, '=')
		if eqIdx < 0 {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name:  strings.TrimSpace(part[:eqIdx]),
			Value: strings.TrimSpace(part[eqIdx+1:]),
		})
	}
	e.httpClient.Jar.SetCookies(baseURL, cookies)

	if err := e.verifyLogin(ctx); err != nil {
		return fmt.Errorf("cookie login: %w", err)
	}

	e.loggedIn = true
	slog.Debug("indexer cookie login successful", "indexer", e.def.ID)
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

	resp, err := e.doRequest(req)
	if err != nil {
		return fmt.Errorf("login test request: %w", err)
	}
	defer resp.Body.Close()

	body, err := e.readBody(resp.Body)
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
	renderedPath, err := RenderTemplate(searchPath, tmplCtx)
	if err != nil {
		return nil, fmt.Errorf("rendering search path: %w", err)
	}

	rendered, err := RenderInputs(e.def.Search.Inputs, tmplCtx)
	if err != nil {
		return nil, fmt.Errorf("rendering search inputs: %w", err)
	}

	searchURL := e.resolveURL(renderedPath)
	params := url.Values{}
	rawSuffix := ""

	for k, v := range rendered {
		if k == "$raw" {
			rawSuffix = v
			continue
		}
		params.Set(k, v)
	}

	encoded := params.Encode()
	if encoded != "" && rawSuffix != "" {
		encoded += "&"
	}
	fullURL := searchURL + "?" + encoded + rawSuffix

	slog.Debug("indexer search request",
		"indexer", e.def.ID,
		"url", redactURL(fullURL),
		"query", query.Q,
		"type", query.Type,
		"categories", categories,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	for name, vals := range e.def.Search.Headers {
		for _, v := range vals {
			rendered, err := RenderTemplate(v, tmplCtx)
			if err != nil {
				return nil, fmt.Errorf("rendering search header %q: %w", name, err)
			}
			req.Header.Set(name, rendered)
		}
	}

	resp, err := e.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	slog.Debug("indexer search response",
		"indexer", e.def.ID,
		"status", resp.StatusCode,
		"content_type", resp.Header.Get("Content-Type"),
	)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	body, err := e.readBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading search response: %w", err)
	}

	slog.Debug("indexer search response body",
		"indexer", e.def.ID,
		"body_length", len(body),
		"body_preview", truncate(string(body), 2000),
	)

	// JSON response path.
	if e.def.Search.Paths[0].Response.Type == "json" {
		return e.parseRowsJSON(body, tmplCtx)
	}

	// HTML response path.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parsing search response: %w", err)
	}

	rowSelector := e.def.Search.Rows.Selector
	rowCount := doc.Find(rowSelector).Length()
	slog.Debug("indexer search row matching",
		"indexer", e.def.ID,
		"row_selector", rowSelector,
		"rows_found", rowCount,
	)

	return e.parseRows(doc, tmplCtx)
}

// FetchDownload fetches a download URL using the engine's authenticated session.
// If the response is HTML instead of a torrent file, it uses the definition's
// download selectors to extract the real download link and fetches that.
func (e *Engine) FetchDownload(ctx context.Context, downloadURL string) ([]byte, error) {
	if !e.loggedIn {
		if err := e.Login(ctx); err != nil {
			return nil, fmt.Errorf("auto-login: %w", err)
		}
	}

	data, err := e.fetchURL(ctx, downloadURL)
	if err != nil {
		return nil, err
	}

	// Torrent files start with 'd' (bencode dict). If not, it's likely HTML.
	if len(data) > 0 && data[0] == 'd' {
		return data, nil
	}

	// Try to extract the real download link from the HTML using download selectors.
	if len(e.def.Download.Selectors) == 0 {
		return nil, fmt.Errorf("response is not a torrent file and no download selectors defined")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("parsing download page: %w", err)
	}

	for _, sel := range e.def.Download.Selectors {
		node := doc.Find(sel.Selector)
		if node.Length() == 0 {
			continue
		}
		link, exists := node.Attr(sel.Attribute)
		if !exists || link == "" {
			continue
		}

		realURL := e.resolveURL(link)
		return e.fetchURL(ctx, realURL)
	}

	return nil, fmt.Errorf("download selectors did not match any link in the response")
}

func (e *Engine) fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}

	// Apply search.headers (e.g. x-milkie-auth) to download requests too.
	if len(e.def.Search.Headers) > 0 {
		tmplCtx := &TemplateContext{
			Config: e.config,
		}
		for name, vals := range e.def.Search.Headers {
			for _, v := range vals {
				rendered, err := RenderTemplate(v, tmplCtx)
				if err != nil {
					continue
				}
				req.Header.Set(name, rendered)
			}
		}
	}

	resp, err := e.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading download response: %w", err)
	}

	return data, nil
}

// readBody reads an HTTP response body, converting from the definition's
// encoding to UTF-8 if specified (e.g. ISO-8859-2 for Hungarian sites).
func (e *Engine) readBody(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, 2<<20)

	if enc := e.def.Encoding; enc != "" && !strings.EqualFold(enc, "UTF-8") {
		encoding, err := ianaindex.IANA.Encoding(enc)
		if err != nil {
			slog.Warn("unknown encoding, reading as raw bytes", "encoding", enc, "indexer", e.def.ID)
			return io.ReadAll(limited)
		}
		return io.ReadAll(transform.NewReader(limited, encoding.NewDecoder()))
	}

	return io.ReadAll(limited)
}

// --- FlareSolverr integration ---

type flareSolverrResponse struct {
	Status   string               `json:"status"`
	Message  string               `json:"message"`
	Solution flareSolverrSolution `json:"solution"`
}

type flareSolverrSolution struct {
	URL     string                `json:"url"`
	Status  int                   `json:"status"`
	Cookies []flareSolverrCookie  `json:"cookies"`
	Headers map[string]string     `json:"headers"`
	Response string               `json:"response"`
}

type flareSolverrCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

// needsFlareSolverr returns true if the definition has an info_flaresolverr setting,
// indicating the indexer is behind Cloudflare protection.
func (e *Engine) needsFlareSolverr() bool {
	for _, s := range e.def.Settings {
		if s.Type == "info_flaresolverr" {
			return true
		}
	}
	return false
}

// doFlareSolverr routes a GET request through FlareSolverr's POST /v1 API.
// It sends the target URL to FlareSolverr, injects returned cookies into the jar,
// and returns a synthetic *http.Response with the solved page body.
func (e *Engine) doFlareSolverr(ctx context.Context, targetURL string) (*http.Response, error) {
	fsURL := e.config["flaresolverr_url"]

	payload, _ := json.Marshal(map[string]any{
		"cmd":        "request.get",
		"url":        targetURL,
		"maxTimeout": 30000,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(fsURL, "/")+"/v1",
		bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating FlareSolverr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FlareSolverr request failed: %w", err)
	}
	defer resp.Body.Close()

	var fsResp flareSolverrResponse
	if err := json.NewDecoder(resp.Body).Decode(&fsResp); err != nil {
		return nil, fmt.Errorf("decoding FlareSolverr response: %w", err)
	}
	if fsResp.Status != "ok" {
		return nil, fmt.Errorf("FlareSolverr error: %s", fsResp.Message)
	}

	// Inject cookies from FlareSolverr into the engine's jar for subsequent requests.
	if baseURL, err := url.Parse(e.baseURL); err == nil {
		var cookies []*http.Cookie
		for _, c := range fsResp.Solution.Cookies {
			cookies = append(cookies, &http.Cookie{
				Name:   c.Name,
				Value:  c.Value,
				Domain: c.Domain,
				Path:   c.Path,
			})
		}
		if len(cookies) > 0 {
			e.httpClient.Jar.SetCookies(baseURL, cookies)
			slog.Debug("injected FlareSolverr cookies", "indexer", e.def.ID, "count", len(cookies))
		}
	}

	// Return a synthetic response with the solved page content.
	synth := &http.Response{
		StatusCode: fsResp.Solution.Status,
		Status:     fmt.Sprintf("%d OK", fsResp.Solution.Status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(fsResp.Solution.Response)),
	}
	if synth.StatusCode == 0 {
		synth.StatusCode = 200
	}
	return synth, nil
}

// doRequest routes a request through FlareSolverr if this is a GET to an indexer
// that needs it and FlareSolverr is configured. POST requests (login) always go direct.
func (e *Engine) doRequest(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet && e.needsFlareSolverr() && e.config["flaresolverr_url"] != "" {
		slog.Debug("routing through FlareSolverr", "indexer", e.def.ID, "url", redactURL(req.URL.String()))
		return e.doFlareSolverr(req.Context(), req.URL.String())
	}
	return e.httpClient.Do(req)
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

	return e.buildSearchResult(fields, tmplCtx)
}

// buildSearchResult applies defaults, renders text templates, and constructs a SearchResult
// from extracted field values. Shared by both HTML and JSON parsing paths.
func (e *Engine) buildSearchResult(fields map[string]string, tmplCtx *TemplateContext) (*SearchResult, error) {
	// Apply defaults for empty optional fields (default values may reference .Result).
	for name, fieldDef := range e.def.Search.Fields {
		if fieldDef.Default == "" || fields[name] != "" {
			continue
		}
		ctx := &TemplateContext{
			Config:     tmplCtx.Config,
			Keywords:   tmplCtx.Keywords,
			Query:      tmplCtx.Query,
			Categories: tmplCtx.Categories,
			Result:     fields,
		}
		rendered, err := RenderTemplate(fieldDef.Default, ctx)
		if err != nil {
			continue
		}
		fields[name] = rendered
	}

	// Render any text fields that reference .Result
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

		// Apply filters to the rendered text value (e.g. urlencode on _apikey).
		if len(fieldDef.Filters) > 0 {
			rendered, err = ApplyFilters(rendered, fieldDef.Filters)
			if err != nil {
				return nil, fmt.Errorf("filtering field %q: %w", name, err)
			}
		}

		fields[name] = rendered
	}

	r := &SearchResult{
		Title:       fields["title"],
		Details:     e.maybeResolveURL(fields["details"]),
		Download:    e.maybeResolveURL(fields["download"]),
		Size:        fields["size"],
		ImdbID:      extractImdbID(fields["imdbid"]),
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

// redactURL strips query parameters from a URL for safe logging.
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid-url]"
	}
	if u.RawQuery != "" {
		u.RawQuery = "[redacted]"
	}
	return u.String()
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}

// --- JSON response support ---

// resolveJSONPath navigates a JSON object/array tree using a dot-path selector.
// Supports: simple keys, dot-separated paths, array indexing (key[N]), and "$" for root.
func resolveJSONPath(data any, path string) (any, error) {
	if path == "$" || path == "" {
		return data, nil
	}
	path = strings.TrimPrefix(path, "$.")

	segments := strings.Split(path, ".")
	current := data

	for _, seg := range segments {
		// Check for array index: "key[N]"
		if bracketIdx := strings.IndexByte(seg, '['); bracketIdx >= 0 {
			key := seg[:bracketIdx]
			indexStr := strings.TrimSuffix(seg[bracketIdx+1:], "]")
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return nil, fmt.Errorf("invalid array index in %q", seg)
			}

			obj, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("expected object for key %q, got %T", key, current)
			}
			arr, ok := obj[key].([]any)
			if !ok {
				return nil, fmt.Errorf("expected array for key %q", key)
			}
			if index < 0 || index >= len(arr) {
				return nil, fmt.Errorf("index %d out of range for %q (len %d)", index, key, len(arr))
			}
			current = arr[index]
			continue
		}

		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object for key %q, got %T", seg, current)
		}
		val, exists := obj[seg]
		if !exists {
			return nil, fmt.Errorf("key %q not found", seg)
		}
		current = val
	}

	return current, nil
}

// jsonValueToString converts a JSON-unmarshalled value to string.
// Numbers are formatted without scientific notation; bools use "True"/"False"
// to match UNIT3D definition case map keys.
func jsonValueToString(val any) string {
	switch v := val.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// parseRowsJSON parses a JSON response body into search results.
func (e *Engine) parseRowsJSON(body []byte, tmplCtx *TemplateContext) ([]SearchResult, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parsing JSON response: %w", err)
	}

	rowsData, err := resolveJSONPath(root, e.def.Search.Rows.Selector)
	if err != nil {
		return nil, fmt.Errorf("resolving rows selector %q: %w", e.def.Search.Rows.Selector, err)
	}

	items, ok := rowsData.([]any)
	if !ok {
		// Single object — wrap in array.
		if obj, ok := rowsData.(map[string]any); ok {
			items = []any{obj}
		} else {
			return nil, fmt.Errorf("rows selector %q resolved to %T, expected array", e.def.Search.Rows.Selector, rowsData)
		}
	}

	attr := e.def.Search.Rows.Attribute
	multiple := e.def.Search.Rows.Multiple

	var results []SearchResult

	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if attr == "" {
			// No attribute: each array element is a row.
			result, err := e.parseRowJSON(obj, nil, tmplCtx)
			if err != nil {
				slog.Debug("skipping JSON row", "error", err, "indexer", e.def.ID)
				continue
			}
			results = append(results, *result)
		} else if !multiple {
			// Attribute without multiple: fields are under item[attribute].
			subVal, exists := obj[attr]
			if !exists {
				continue
			}
			subObj, ok := subVal.(map[string]any)
			if !ok {
				continue
			}
			result, err := e.parseRowJSON(subObj, nil, tmplCtx)
			if err != nil {
				slog.Debug("skipping JSON row", "error", err, "indexer", e.def.ID)
				continue
			}
			results = append(results, *result)
		} else {
			// Attribute with multiple: parent[attribute] is an array of sub-items.
			subVal, exists := obj[attr]
			if !exists {
				continue
			}
			subArr, ok := subVal.([]any)
			if !ok {
				continue
			}
			for _, subItem := range subArr {
				subObj, ok := subItem.(map[string]any)
				if !ok {
					continue
				}
				result, err := e.parseRowJSON(subObj, obj, tmplCtx)
				if err != nil {
					slog.Debug("skipping JSON sub-row", "error", err, "indexer", e.def.ID)
					continue
				}
				results = append(results, *result)
			}
		}
	}

	return results, nil
}

// parseRowJSON extracts fields from a single JSON object and builds a SearchResult.
func (e *Engine) parseRowJSON(item map[string]any, parent map[string]any, tmplCtx *TemplateContext) (*SearchResult, error) {
	fields := make(map[string]string)

	for name, fieldDef := range e.def.Search.Fields {
		val, err := e.extractFieldJSON(item, parent, fieldDef)
		if err != nil {
			if fieldDef.Optional {
				continue
			}
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields[name] = val
	}

	return e.buildSearchResult(fields, tmplCtx)
}

// extractFieldJSON extracts a single field value from a JSON object.
func (e *Engine) extractFieldJSON(item map[string]any, parent map[string]any, fieldDef FieldDef) (string, error) {
	// Text-only field (no selector): template rendering happens in buildSearchResult.
	if fieldDef.Text != "" && fieldDef.Selector == "" {
		return fieldDef.Text, nil
	}

	// No selector and no text: nothing to extract.
	if fieldDef.Selector == "" {
		return "", nil
	}

	// Resolve the value from JSON.
	var raw any
	var err error

	if strings.HasPrefix(fieldDef.Selector, "..") {
		// Parent traversal: strip ".." and resolve against parent.
		if parent == nil {
			return "", nil
		}
		raw, err = resolveJSONPath(parent, fieldDef.Selector[2:])
	} else {
		raw, err = resolveJSONPath(item, fieldDef.Selector)
	}

	if err != nil {
		if fieldDef.Optional {
			return "", nil
		}
		return "", fmt.Errorf("resolving selector %q: %w", fieldDef.Selector, err)
	}

	value := jsonValueToString(raw)
	value = strings.TrimSpace(value)

	// Case mapping: compare extracted value against case keys.
	if len(fieldDef.Case) > 0 {
		if mapped, ok := fieldDef.Case[value]; ok {
			value = mapped
		} else if wildcard, ok := fieldDef.Case["*"]; ok {
			value = wildcard
		}
	}

	if len(fieldDef.Filters) > 0 {
		value, err = ApplyFilters(value, fieldDef.Filters)
		if err != nil {
			return "", fmt.Errorf("applying filters: %w", err)
		}
	}

	return value, nil
}
