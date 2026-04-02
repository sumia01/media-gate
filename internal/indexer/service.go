package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/sumia01/media-gate/internal/indexer/cardigann"
	"github.com/sumia01/media-gate/internal/indexer/definitions"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

// Service manages indexer CRUD, definition loading, and multi-indexer search.
type Service struct {
	store       store.Store
	settingsSvc *settings.Service
	defs        map[string]*cardigann.Definition
	engines     map[uint]*engineEntry
	mu          sync.Mutex
}

type engineEntry struct {
	engine *cardigann.Engine
	mu     sync.Mutex
}

// DefinitionInfo describes an available indexer definition.
type DefinitionInfo struct {
	ID          string
	Name        string
	Description string
	Language    string
	Type        string
	Links       []string
	Settings    []SettingFieldInfo
}

// SettingFieldInfo describes a single setting field from a definition.
type SettingFieldInfo struct {
	Name    string
	Type    string
	Label   string
	Default string
}

// IndexerInfo is the API-facing view of a configured indexer.
type IndexerInfo struct {
	ID           uint
	Name         string
	DefinitionID string
	Enabled      bool
	Settings     map[string]string
	Priority     int
	SeedMinRatio float64
	SeedMinTime  int
}

// TorrentResult is a search result from an indexer.
type TorrentResult struct {
	IndexerID            uint
	IndexerName          string
	Title                string
	DetailsURL           string
	DownloadURL          string
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

// SearchParams controls an indexer search.
type SearchParams struct {
	Query      string
	ImdbID     string
	Type       string // search, tv-search, movie-search
	Season     string
	Episode    string
	Categories []string
	IndexerIDs []uint
	Limit      int
}

// NewService loads built-in definitions and creates the indexer service.
func NewService(s store.Store, settingsSvc *settings.Service) (*Service, error) {
	raw, err := definitions.LoadBuiltin()
	if err != nil {
		return nil, fmt.Errorf("loading built-in definitions: %w", err)
	}

	defs := make(map[string]*cardigann.Definition, len(raw))
	for id, data := range raw {
		def, err := cardigann.ParseDefinition(data)
		if err != nil {
			return nil, fmt.Errorf("parsing definition %q: %w", id, err)
		}
		defs[id] = def
	}

	slog.Info("indexer definitions loaded", "count", len(defs))

	return &Service{
		store:       s,
		settingsSvc: settingsSvc,
		defs:        defs,
		engines:     make(map[uint]*engineEntry),
	}, nil
}

// ListDefinitions returns all available indexer definitions.
func (s *Service) ListDefinitions() []DefinitionInfo {
	result := make([]DefinitionInfo, 0, len(s.defs))
	for _, def := range s.defs {
		result = append(result, definitionToInfo(def))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// GetDefinition returns a single definition by ID.
func (s *Service) GetDefinition(id string) (*DefinitionInfo, error) {
	def, ok := s.defs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	info := definitionToInfo(def)
	return &info, nil
}

// Create adds a new indexer configuration.
func (s *Service) Create(name, definitionID string, settings map[string]string, priority int, seedMinRatio float64, seedMinTime int) (*IndexerInfo, error) {
	if _, ok := s.defs[definitionID]; !ok {
		return nil, fmt.Errorf("unknown definition: %q", definitionID)
	}

	pwFields := s.passwordFields(definitionID)
	nonSensitive := make(map[string]string, len(settings))
	sensitive := make(map[string]string)
	for k, v := range settings {
		if pwFields[k] {
			sensitive[k] = v
		} else {
			nonSensitive[k] = v
		}
	}

	settingsJSON, err := json.Marshal(nonSensitive)
	if err != nil {
		return nil, fmt.Errorf("marshalling settings: %w", err)
	}

	indexer := &store.Indexer{
		Name:         name,
		DefinitionID: definitionID,
		Enabled:      true,
		Settings:     string(settingsJSON),
		Priority:     priority,
		SeedMinRatio: seedMinRatio,
		SeedMinTime:  seedMinTime,
	}
	if err := s.store.CreateIndexer(indexer); err != nil {
		return nil, fmt.Errorf("creating indexer: %w", err)
	}

	for field, value := range sensitive {
		if err := s.settingsSvc.SetIndexerSecret(indexer.ID, field, value); err != nil {
			_ = s.store.DeleteIndexer(indexer.ID)
			return nil, fmt.Errorf("saving indexer secret %q: %w", field, err)
		}
	}

	return s.indexerToInfo(indexer)
}

// Get returns a single indexer by ID.
func (s *Service) Get(id uint) (*IndexerInfo, error) {
	indexer, err := s.store.GetIndexer(id)
	if err != nil {
		return nil, err
	}
	return s.indexerToInfo(indexer)
}

// List returns all configured indexers.
func (s *Service) List() ([]IndexerInfo, error) {
	indexers, err := s.store.ListIndexers()
	if err != nil {
		return nil, err
	}
	result := make([]IndexerInfo, 0, len(indexers))
	for _, idx := range indexers {
		info, err := s.indexerToInfo(&idx)
		if err != nil {
			continue
		}
		result = append(result, *info)
	}
	return result, nil
}

// Update modifies an existing indexer.
func (s *Service) Update(id uint, name *string, settings map[string]string, enabled *bool, priority *int, seedMinRatio *float64, seedMinTime *int) (*IndexerInfo, error) {
	indexer, err := s.store.GetIndexer(id)
	if err != nil {
		return nil, err
	}

	if name != nil {
		indexer.Name = *name
	}
	if enabled != nil {
		indexer.Enabled = *enabled
	}
	if priority != nil {
		indexer.Priority = *priority
	}
	if len(settings) > 0 {
		merged, err := s.mergeSettings(indexer, settings)
		if err != nil {
			return nil, fmt.Errorf("merging settings: %w", err)
		}

		pwFields := s.passwordFields(indexer.DefinitionID)
		nonSensitive := make(map[string]string, len(merged))
		for k, v := range merged {
			if pwFields[k] {
				if err := s.settingsSvc.SetIndexerSecret(indexer.ID, k, v); err != nil {
					return nil, fmt.Errorf("saving indexer secret %q: %w", k, err)
				}
			} else {
				nonSensitive[k] = v
			}
		}

		settingsJSON, err := json.Marshal(nonSensitive)
		if err != nil {
			return nil, fmt.Errorf("marshalling settings: %w", err)
		}
		indexer.Settings = string(settingsJSON)
		s.invalidateEngine(id)
	}
	if seedMinRatio != nil {
		indexer.SeedMinRatio = *seedMinRatio
	}
	if seedMinTime != nil {
		indexer.SeedMinTime = *seedMinTime
	}

	if err := s.store.UpdateIndexer(indexer); err != nil {
		return nil, fmt.Errorf("updating indexer: %w", err)
	}
	return s.indexerToInfo(indexer)
}

// Delete removes an indexer.
func (s *Service) Delete(id uint) error {
	s.invalidateEngine(id)
	_ = s.settingsSvc.DeleteIndexerSecrets(id)
	return s.store.DeleteIndexer(id)
}

// TestConnection tests whether an indexer can log in.
func (s *Service) TestConnection(id uint, overrideSettings map[string]string) (bool, string, error) {
	indexer, err := s.store.GetIndexer(id)
	if err != nil {
		return false, "", err
	}

	def, ok := s.defs[indexer.DefinitionID]
	if !ok {
		return false, "unknown definition", nil
	}

	settings, err := s.mergeSettings(indexer, overrideSettings)
	if err != nil {
		return false, "", err
	}

	engine, err := cardigann.NewEngine(def, settings)
	if err != nil {
		return false, err.Error(), nil
	}

	if err := engine.TestConnection(context.Background()); err != nil {
		return false, err.Error(), nil
	}

	return true, "Connection successful", nil
}

// Search queries multiple indexers in parallel and aggregates results.
func (s *Service) Search(ctx context.Context, params SearchParams) ([]TorrentResult, error) {
	indexers, err := s.store.ListIndexers()
	if err != nil {
		return nil, fmt.Errorf("listing indexers: %w", err)
	}

	// Filter to enabled indexers or specific IDs.
	wantIDs := make(map[uint]bool, len(params.IndexerIDs))
	for _, id := range params.IndexerIDs {
		wantIDs[id] = true
	}

	var targets []store.Indexer
	for _, idx := range indexers {
		if len(wantIDs) > 0 {
			if wantIDs[idx.ID] {
				targets = append(targets, idx)
			}
		} else if idx.Enabled {
			targets = append(targets, idx)
		}
	}

	if len(targets) == 0 {
		return []TorrentResult{}, nil
	}

	query := cardigann.SearchQuery{
		Type:       params.Type,
		Q:          params.Query,
		IMDBID:     params.ImdbID,
		Season:     params.Season,
		Ep:         params.Episode,
		Categories: params.Categories,
	}

	type indexerResults struct {
		results []TorrentResult
	}

	resultsCh := make(chan indexerResults, len(targets))
	sem := make(chan struct{}, 3)

	var wg sync.WaitGroup
	for _, idx := range targets {
		wg.Add(1)
		go func(idx store.Indexer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			entry, err := s.getOrCreateEngine(&idx)
			if err != nil {
				slog.Warn("failed to create indexer engine", "indexer", idx.Name, "error", err)
				return
			}

			entry.mu.Lock()
			results, err := entry.engine.Search(ctx, query)
			entry.mu.Unlock()

			if err != nil {
				slog.Warn("indexer search failed", "indexer", idx.Name, "error", err)
				return
			}

			var converted []TorrentResult
			for _, r := range results {
				converted = append(converted, TorrentResult{
					IndexerID:            idx.ID,
					IndexerName:          idx.Name,
					Title:                r.Title,
					DetailsURL:           r.Details,
					DownloadURL:          r.Download,
					Size:                 r.Size,
					Seeders:              r.Seeders,
					Leechers:             r.Leechers,
					Date:                 r.Date,
					Category:             r.Category,
					CategoryDesc:         r.CategoryDesc,
					ImdbID:               r.ImdbID,
					DownloadVolumeFactor: r.DownloadVolumeFactor,
					UploadVolumeFactor:   r.UploadVolumeFactor,
				})
			}
			resultsCh <- indexerResults{results: converted}
		}(idx)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var all []TorrentResult
	for ir := range resultsCh {
		all = append(all, ir.results...)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Seeders > all[j].Seeders })

	if params.Limit > 0 && len(all) > params.Limit {
		all = all[:params.Limit]
	}

	return all, nil
}

func (s *Service) getOrCreateEngine(indexer *store.Indexer) (*engineEntry, error) {
	s.mu.Lock()
	if entry, ok := s.engines[indexer.ID]; ok {
		s.mu.Unlock()
		return entry, nil
	}
	s.mu.Unlock()

	def, ok := s.defs[indexer.DefinitionID]
	if !ok {
		return nil, fmt.Errorf("unknown definition: %q", indexer.DefinitionID)
	}

	cfg, err := parseSettings(indexer.Settings)
	if err != nil {
		return nil, err
	}

	secrets, err := s.settingsSvc.GetIndexerSecrets(indexer.ID)
	if err != nil {
		return nil, fmt.Errorf("fetching indexer secrets: %w", err)
	}
	for k, v := range secrets {
		cfg[k] = v
	}

	engine, err := cardigann.NewEngine(def, cfg)
	if err != nil {
		return nil, err
	}

	entry := &engineEntry{engine: engine}
	s.mu.Lock()
	s.engines[indexer.ID] = entry
	s.mu.Unlock()

	return entry, nil
}

func (s *Service) invalidateEngine(id uint) {
	s.mu.Lock()
	delete(s.engines, id)
	s.mu.Unlock()
}

// FetchTorrent downloads a .torrent file using the indexer's authenticated session.
func (s *Service) FetchTorrent(ctx context.Context, indexerID uint, downloadURL string) ([]byte, error) {
	indexer, err := s.store.GetIndexer(indexerID)
	if err != nil {
		return nil, fmt.Errorf("getting indexer: %w", err)
	}

	entry, err := s.getOrCreateEngine(indexer)
	if err != nil {
		return nil, fmt.Errorf("creating engine: %w", err)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	data, err := entry.engine.FetchDownload(ctx, downloadURL)
	if err != nil {
		return nil, fmt.Errorf("fetching torrent from %s: %w", indexer.Name, err)
	}

	return data, nil
}

func (s *Service) mergeSettings(indexer *store.Indexer, overrides map[string]string) (map[string]string, error) {
	cfg, err := parseSettings(indexer.Settings)
	if err != nil {
		return nil, err
	}
	secrets, err := s.settingsSvc.GetIndexerSecrets(indexer.ID)
	if err != nil {
		return nil, fmt.Errorf("fetching indexer secrets: %w", err)
	}
	for k, v := range secrets {
		cfg[k] = v
	}
	for k, v := range overrides {
		if strings.HasPrefix(v, "****") {
			continue // skip masked values — keep the stored original
		}
		cfg[k] = v
	}
	return cfg, nil
}

func (s *Service) indexerToInfo(indexer *store.Indexer) (*IndexerInfo, error) {
	cfg, err := parseSettings(indexer.Settings)
	if err != nil {
		return nil, err
	}

	secrets, err := s.settingsSvc.GetIndexerSecrets(indexer.ID)
	if err != nil {
		return nil, err
	}
	for k, v := range secrets {
		cfg[k] = v
	}

	s.maskSettings(indexer.DefinitionID, cfg)

	return &IndexerInfo{
		ID:           indexer.ID,
		Name:         indexer.Name,
		DefinitionID: indexer.DefinitionID,
		Enabled:      indexer.Enabled,
		Settings:     cfg,
		Priority:     indexer.Priority,
		SeedMinRatio: indexer.SeedMinRatio,
		SeedMinTime:  indexer.SeedMinTime,
	}, nil
}

func (s *Service) maskSettings(defID string, settings map[string]string) {
	def, ok := s.defs[defID]
	if !ok {
		return
	}
	for _, field := range def.Settings {
		if field.Type != "password" {
			continue
		}
		if val, exists := settings[field.Name]; exists && val != "" {
			settings[field.Name] = maskValue(val)
		}
	}
}

// passwordFields returns a set of field names that are password-type for a given definition.
func (s *Service) passwordFields(defID string) map[string]bool {
	def, ok := s.defs[defID]
	if !ok {
		return nil
	}
	m := make(map[string]bool)
	for _, f := range def.Settings {
		if f.Type == "password" {
			m[f.Name] = true
		}
	}
	return m
}

// MigrateCredentials moves password fields from Indexer.Settings JSON into the
// Settings table and strips them from the JSON column. Idempotent.
func (s *Service) MigrateCredentials() error {
	indexers, err := s.store.ListIndexers()
	if err != nil {
		return fmt.Errorf("listing indexers: %w", err)
	}
	for _, idx := range indexers {
		pwFields := s.passwordFields(idx.DefinitionID)
		if len(pwFields) == 0 {
			continue
		}
		cfg, err := parseSettings(idx.Settings)
		if err != nil {
			slog.Warn("skipping indexer with unparseable settings", "id", idx.ID, "error", err)
			continue
		}
		changed := false
		for field := range pwFields {
			val, exists := cfg[field]
			if !exists || val == "" {
				continue
			}
			// Check if already migrated
			existing, err := s.settingsSvc.GetIndexerSecrets(idx.ID)
			if err == nil {
				if _, ok := existing[field]; ok {
					// Already in Settings table — remove from JSON
					delete(cfg, field)
					changed = true
					continue
				}
			}
			if err := s.settingsSvc.SetIndexerSecret(idx.ID, field, val); err != nil {
				return fmt.Errorf("migrating indexer %d field %s: %w", idx.ID, field, err)
			}
			delete(cfg, field)
			changed = true
			slog.Info("migrated indexer credential to settings table", "indexer_id", idx.ID, "field", field)
		}
		if changed {
			jsonBytes, err := json.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("re-marshalling indexer %d settings: %w", idx.ID, err)
			}
			idx.Settings = string(jsonBytes)
			if err := s.store.UpdateIndexer(&idx); err != nil {
				return fmt.Errorf("updating indexer %d: %w", idx.ID, err)
			}
		}
	}
	return nil
}

func parseSettings(raw string) (map[string]string, error) {
	if raw == "" || raw == "{}" {
		return make(map[string]string), nil
	}
	var settings map[string]string
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, fmt.Errorf("parsing indexer settings: %w", err)
	}
	return settings, nil
}

func maskValue(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

func definitionToInfo(def *cardigann.Definition) DefinitionInfo {
	settings := make([]SettingFieldInfo, 0)
	for _, s := range def.Settings {
		if s.Type == "info" {
			continue
		}
		settings = append(settings, SettingFieldInfo{
			Name:    s.Name,
			Type:    s.Type,
			Label:   s.Label,
			Default: s.Default,
		})
	}
	return DefinitionInfo{
		ID:          def.ID,
		Name:        def.Name,
		Description: def.Description,
		Language:    def.Language,
		Type:        def.Type,
		Links:       def.Links,
		Settings:    settings,
	}
}
