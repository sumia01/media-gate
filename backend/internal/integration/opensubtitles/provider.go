package opensubtitles

import (
	"net/http"
	"sync"
)

// SettingsGetter retrieves a setting value by key. Avoids circular import
// with the settings package.
type SettingsGetter interface {
	Get(key string) (string, error)
}

const (
	keyApiKey   = "opensubtitles_api_key"
	keyUsername = "opensubtitles_username"
	keyPassword = "opensubtitles_password"
)

// Provider creates and caches an OpenSubtitles Client from settings.
// Call Invalidate when OpenSubtitles settings change to force re-creation on next access.
type Provider struct {
	settings   SettingsGetter
	httpClient *http.Client
	client     *Client
	mu         sync.Mutex
}

// NewProvider creates a Provider that reads OpenSubtitles credentials from settings.
func NewProvider(sg SettingsGetter, httpClient *http.Client) *Provider {
	return &Provider{settings: sg, httpClient: httpClient}
}

// Client returns a cached client, creating one from settings on first call.
func (p *Provider) Client() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return p.client, nil
	}

	apiKey, err := p.settings.Get(keyApiKey)
	if err != nil {
		return nil, err
	}
	username, err := p.settings.Get(keyUsername)
	if err != nil {
		return nil, err
	}
	password, err := p.settings.Get(keyPassword)
	if err != nil {
		return nil, err
	}

	p.client = NewClient(apiKey, username, password, p.httpClient)
	return p.client, nil
}

// Invalidate clears the cached client so the next Client() call re-reads settings.
func (p *Provider) Invalidate() {
	p.mu.Lock()
	p.client = nil
	p.mu.Unlock()
}
