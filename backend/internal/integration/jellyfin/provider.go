package jellyfin

import (
	"errors"
	"net/http"
	"sync"
)

type SettingsGetter interface {
	Get(key string) (string, error)
}

// Provider lazily caches a client, invalidated whenever connection settings change.
type Provider struct {
	settings   SettingsGetter
	urlKey     string
	apiKeyKey  string
	httpClient *http.Client
	mu         sync.Mutex
	client     *Client
}

func NewProvider(settings SettingsGetter, urlKey, apiKeyKey string, httpClient *http.Client) *Provider {
	return &Provider{settings: settings, urlKey: urlKey, apiKeyKey: apiKeyKey, httpClient: httpClient}
}

func (p *Provider) Client() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		return p.client, nil
	}
	url, err := p.settings.Get(p.urlKey)
	if err != nil || url == "" {
		return nil, errors.New("Jellyfin URL not configured")
	}
	key, err := p.settings.Get(p.apiKeyKey)
	if err != nil || key == "" {
		return nil, errors.New("Jellyfin API key not configured")
	}
	p.client = NewClient(url, key, p.httpClient)
	return p.client, nil
}

func (p *Provider) Invalidate() {
	p.mu.Lock()
	p.client = nil
	p.mu.Unlock()
}

func (p *Provider) RefreshLibrary(id string) error {
	client, err := p.Client()
	if err != nil {
		return err
	}
	return client.RefreshLibrary(id)
}
