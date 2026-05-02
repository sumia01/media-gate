package plex

import (
	"fmt"
	"net/http"
	"sync"
)

// SettingsGetter retrieves a setting value by key.
type SettingsGetter interface {
	Get(key string) (string, error)
}

// Provider creates and caches a Plex Client from settings.
// Call Invalidate when Plex settings change to force re-creation on next access.
type Provider struct {
	settings   SettingsGetter
	urlKey     string
	tokenKey   string
	httpClient *http.Client
	client     *Client
	mu         sync.Mutex
}

// NewProvider creates a Provider that reads the given settings keys.
func NewProvider(sg SettingsGetter, urlKey, tokenKey string, httpClient *http.Client) *Provider {
	return &Provider{
		settings:   sg,
		urlKey:     urlKey,
		tokenKey:   tokenKey,
		httpClient: httpClient,
	}
}

// Client returns a cached Plex client, creating one from settings on first call.
func (p *Provider) Client() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return p.client, nil
	}

	url, err := p.settings.Get(p.urlKey)
	if err != nil || url == "" {
		return nil, fmt.Errorf("plex URL not configured")
	}
	token, err := p.settings.Get(p.tokenKey)
	if err != nil || token == "" {
		return nil, fmt.Errorf("plex token not configured")
	}

	p.client = NewClient(url, token, p.httpClient)
	return p.client, nil
}

// Invalidate clears the cached client so the next Client() call re-reads settings.
func (p *Provider) Invalidate() {
	p.mu.Lock()
	p.client = nil
	p.mu.Unlock()
}
