package qbittorrent

import (
	"net/http"
	"sync"
)

// SettingsGetter retrieves a setting value by key. Avoids circular import
// with the settings package.
type SettingsGetter interface {
	Get(key string) (string, error)
}

// Provider creates and caches a qBittorrent Client from settings.
// Call Invalidate when qBit settings change to force re-creation on next access.
type Provider struct {
	settings   SettingsGetter
	urlKey     string
	userKey    string
	passKey    string
	httpClient *http.Client
	client     *Client
	mu         sync.Mutex
}

// NewProvider creates a Provider that reads the given settings keys to construct clients.
func NewProvider(sg SettingsGetter, urlKey, userKey, passKey string, httpClient *http.Client) *Provider {
	return &Provider{
		settings:   sg,
		urlKey:     urlKey,
		userKey:    userKey,
		passKey:    passKey,
		httpClient: httpClient,
	}
}

// Client returns a cached qBittorrent client, creating one from settings on first call.
func (p *Provider) Client() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return p.client, nil
	}

	url, err := p.settings.Get(p.urlKey)
	if err != nil {
		return nil, err
	}
	username, err := p.settings.Get(p.userKey)
	if err != nil {
		return nil, err
	}
	password, err := p.settings.Get(p.passKey)
	if err != nil {
		return nil, err
	}

	p.client = NewClient(url, username, password, p.httpClient)
	return p.client, nil
}

// Invalidate clears the cached client so the next Client() call re-reads settings.
func (p *Provider) Invalidate() {
	p.mu.Lock()
	p.client = nil
	p.mu.Unlock()
}
