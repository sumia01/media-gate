package apiv1

import (
	"encoding/json"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

func userToAPI(u *store.User) UserProfile {
	p := UserProfile{
		Id:        int64(u.ID),
		Email:     openapi_types.Email(u.Email),
		IsAdmin:   u.IsAdmin,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
	if u.FirstName != "" {
		p.FirstName = &u.FirstName
	}
	if u.LastName != "" {
		p.LastName = &u.LastName
	}
	if u.BirthYear != nil {
		p.BirthYear = u.BirthYear
	}
	return p
}

func libraryToAPI(lib *store.Library) Library {
	apiLib := Library{
		Id:        int64(lib.ID),
		Name:      lib.Name,
		Path:      lib.Path,
		MediaType: LibraryMediaType(lib.MediaType),
		CreatedAt: lib.CreatedAt,
		UpdatedAt: lib.UpdatedAt,
	}
	if lib.MediaProfileID != nil {
		mpID := int64(*lib.MediaProfileID)
		apiLib.MediaProfileId = &mpID
	}
	return apiLib
}

func mediaItemToAPI(item *store.MediaItem, meta *store.MediaMetadata) MediaItem {
	apiItem := MediaItem{
		Id:        int64(item.ID),
		LibraryId: int64(item.LibraryID),
		Title:     item.Title,
		MediaType: MediaItemMediaType(item.MediaType),
		Status:    MediaItemStatus(item.Status),
		Source:    MediaItemSource(item.Source),
		Year:      item.Year,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
	if item.MediaProfileID != nil {
		mpID := int64(*item.MediaProfileID)
		apiItem.MediaProfileId = &mpID
	}
	if item.Monitored {
		apiItem.Monitored = &item.Monitored
	}
	mns := item.MonitorNewSeasons
	apiItem.MonitorNewSeasons = &mns
	apiItem.MonitorSearchStartedAt = item.MonitorSearchStartedAt
	if item.PreferredRelease != "" {
		apiItem.PreferredRelease = &item.PreferredRelease
	}
	if meta != nil {
		apiMeta := mediaMetadataToAPI(meta)
		apiItem.Metadata = &apiMeta
	}
	return apiItem
}

func mediaMetadataToAPI(meta *store.MediaMetadata) MediaMetadata {
	m := MediaMetadata{
		Id:         int64(meta.ID),
		Source:     MediaMetadataSource(meta.Source),
		ExternalId: meta.ExternalID,
		Title:      meta.Title,
		Confidence: float32(meta.Confidence),
	}
	if meta.ImdbID != "" {
		m.ImdbId = &meta.ImdbID
	}
	if meta.Overview != "" {
		m.Overview = &meta.Overview
	}
	if meta.PosterPath != "" {
		m.PosterPath = &meta.PosterPath
	}
	if meta.Genres != "" {
		m.Genres = &meta.Genres
	}
	if meta.Year != nil {
		m.Year = meta.Year
	}
	if meta.Rating != nil {
		r := float32(*meta.Rating)
		m.Rating = &r
	}
	if meta.Status != "" {
		m.Status = &meta.Status
	}
	if meta.Runtime != nil {
		m.Runtime = meta.Runtime
	}
	if meta.Seasons != nil {
		m.Seasons = meta.Seasons
	}
	if meta.ReleaseDate != "" {
		m.ReleaseDate = &meta.ReleaseDate
	}
	if meta.TrailerURL != "" {
		m.TrailerUrl = &meta.TrailerURL
	}
	if meta.Credits != "" {
		var credits []CreditPerson
		if err := json.Unmarshal([]byte(meta.Credits), &credits); err == nil {
			m.Credits = &credits
		}
	}
	return m
}

func mediaProfileToAPI(p *store.MediaProfile) MediaProfile {
	api := MediaProfile{
		Id:        int64(p.ID),
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}

	// Language mode
	mode := MediaProfileLanguageMode(p.LanguageMode)
	if mode == "" {
		mode = MediaProfileLanguageModeOr
	}
	api.LanguageMode = mode

	// Parse JSON arrays back to slices
	var resolutions []string
	if err := json.Unmarshal([]byte(p.Resolutions), &resolutions); err == nil {
		api.Resolutions = resolutions
	}
	var languages []string
	if err := json.Unmarshal([]byte(p.Languages), &languages); err == nil {
		api.Languages = languages
	}
	if p.Sources != "" {
		var sources []string
		if err := json.Unmarshal([]byte(p.Sources), &sources); err == nil {
			api.Sources = &sources
		}
	}
	if p.ExcludeTags != "" {
		var tags []string
		if err := json.Unmarshal([]byte(p.ExcludeTags), &tags); err == nil {
			api.ExcludeTags = &tags
		}
	}

	return api
}

func jobToAPI(j *jobqueue.Job) Job {
	apiJob := Job{
		Id:          j.ID,
		Type:        JobType(j.Type),
		Status:      JobStatus(j.Status),
		CreatedAt:   j.CreatedAt,
		StartedAt:   j.StartedAt,
		CompletedAt: j.CompletedAt,
	}
	if j.LibraryID != 0 {
		libID := int64(j.LibraryID)
		apiJob.LibraryId = &libID
	}
	if j.LibraryName != "" {
		apiJob.LibraryName = &j.LibraryName
	}
	if j.Error != "" {
		apiJob.Error = &j.Error
	}
	if j.Progress != nil {
		apiJob.Progress = &struct {
			Current *int    `json:"current,omitempty"`
			Message *string `json:"message,omitempty"`
			Total   *int    `json:"total,omitempty"`
		}{
			Current: &j.Progress.Current,
			Message: &j.Progress.Message,
			Total:   &j.Progress.Total,
		}
	}
	return apiJob
}

func settingsToAPI(items []store.Setting, svc *settings.Service) Settings {
	var s Settings
	for _, item := range items {
		v := item.Value
		switch item.Key {
		case settings.KeyTMDBApiKey:
			s.TmdbApiKey = &v
		case settings.KeyTVDBApiKey:
			s.TvdbApiKey = &v
		case settings.KeyMetadataPrimarySource:
			src := SettingsMetadataPrimarySource(v)
			s.MetadataPrimarySource = &src
		case settings.KeyTMDBRateLimit:
			if n, err := strconv.Atoi(v); err == nil {
				s.TmdbRateLimit = &n
			}
		case settings.KeyTVDBRateLimit:
			if n, err := strconv.Atoi(v); err == nil {
				s.TvdbRateLimit = &n
			}
		case settings.KeyQBitURL:
			s.QbitUrl = &v
		case settings.KeyQBitUsername:
			s.QbitUsername = &v
		case settings.KeyQBitPassword:
			s.QbitPassword = &v
		case settings.KeyQBitDownloadPath:
			s.QbitDownloadPath = &v
		case settings.KeyQBitSavePath:
			s.QbitSavePath = &v
		case settings.KeyQBitCategory:
			s.QbitCategory = &v
		case settings.KeyMonitorSeasonPackPref:
			pref := SettingsMonitorSeasonPackPreference(v)
			s.MonitorSeasonPackPreference = &pref
		case settings.KeyWorkerMonitorInterval:
			if n, err := strconv.Atoi(v); err == nil {
				s.WorkerMonitorInterval = &n
			}
		case settings.KeyWorkerDownloadInterval:
			if n, err := strconv.Atoi(v); err == nil {
				s.WorkerDownloadInterval = &n
			}
		case settings.KeyWorkerImporterInterval:
			if n, err := strconv.Atoi(v); err == nil {
				s.WorkerImporterInterval = &n
			}
		case settings.KeyWorkerMetadataRefreshInterval:
			if n, err := strconv.Atoi(v); err == nil {
				s.WorkerMetadataRefreshInterval = &n
			}
		case settings.KeyWorkerUpdateCheckInterval:
			if n, err := strconv.Atoi(v); err == nil {
				s.WorkerUpdateCheckInterval = &n
			}
		case settings.KeyLibraryBasePath:
			s.LibraryBasePath = &v
		case settings.KeyOnboardingStep:
			if n, err := strconv.Atoi(v); err == nil {
				s.OnboardingStep = &n
			}
		case settings.KeyOnboardingCompleted:
			b := v == "true"
			s.OnboardingCompleted = &b
		case settings.KeyFlareSolverrURL:
			s.FlaresolverrUrl = &v
		case settings.KeyDiscordWebhookURL:
			s.DiscordWebhookUrl = &v
		case settings.KeyGlobalExcludeTags:
			var tags []string
			if err := json.Unmarshal([]byte(v), &tags); err == nil {
				s.GlobalExcludeTags = &tags
			}
		case settings.KeyWatchedListMode:
			m := SettingsWatchedListMode(v)
			s.WatchedListMode = &m
		case settings.KeyOpenSubtitlesApiKey:
			s.OpensubtitlesApiKey = &v
		case settings.KeyOpenSubtitlesUsername:
			s.OpensubtitlesUsername = &v
		case settings.KeyOpenSubtitlesPassword:
			s.OpensubtitlesPassword = &v
		case settings.KeyOpenSubtitlesRateLimit:
			if n, err := strconv.Atoi(v); err == nil {
				s.OpensubtitlesRateLimit = &n
			}
		case settings.KeySubtitleLanguages:
			var langs []string
			if err := json.Unmarshal([]byte(v), &langs); err == nil {
				s.SubtitleLanguages = &langs
			}
		case settings.KeySubtitleAutoSearch:
			b := v == "true"
			s.SubtitleAutoSearch = &b
		case settings.KeyOTelEnabled:
			b := v == "true"
			s.OtelEnabled = &b
		case settings.KeyOTelEndpoint:
			s.OtelEndpoint = &v
		case settings.KeyOTelService:
			s.OtelService = &v
		case settings.KeyOTelLogLevel:
			lvl := SettingsOtelLogLevel(v)
			s.OtelLogLevel = &lvl
		case settings.KeyPlexURL:
			s.PlexUrl = &v
		case settings.KeyPlexToken:
			s.PlexToken = &v
		}
	}
	if svc.HasEnvFallback(settings.KeyTMDBApiKey) {
		t := true
		s.TmdbApiKeyFromEnv = &t
	}
	if svc.HasEnvFallback(settings.KeyTVDBApiKey) {
		t := true
		s.TvdbApiKeyFromEnv = &t
	}
	// Always include the resolved base path (may come from env fallback, not DB).
	basePath := svc.BasePath()
	s.LibraryBasePath = &basePath
	if svc.HasEnvFallback(settings.KeyLibraryBasePath) {
		t := true
		s.LibraryBasePathFromEnv = &t
	}
	return s
}

func settingsFromAPI(s *Settings) []settings.KeyValue {
	var kvs []settings.KeyValue
	if s.TmdbApiKey != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyTMDBApiKey, Value: *s.TmdbApiKey})
	}
	if s.TvdbApiKey != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyTVDBApiKey, Value: *s.TvdbApiKey})
	}
	if s.MetadataPrimarySource != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyMetadataPrimarySource, Value: string(*s.MetadataPrimarySource)})
	}
	if s.TmdbRateLimit != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyTMDBRateLimit, Value: strconv.Itoa(*s.TmdbRateLimit)})
	}
	if s.TvdbRateLimit != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyTVDBRateLimit, Value: strconv.Itoa(*s.TvdbRateLimit)})
	}
	if s.QbitUrl != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyQBitURL, Value: *s.QbitUrl})
	}
	if s.QbitUsername != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyQBitUsername, Value: *s.QbitUsername})
	}
	if s.QbitPassword != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyQBitPassword, Value: *s.QbitPassword})
	}
	if s.QbitDownloadPath != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyQBitDownloadPath, Value: *s.QbitDownloadPath})
	}
	if s.QbitSavePath != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyQBitSavePath, Value: *s.QbitSavePath})
	}
	if s.QbitCategory != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyQBitCategory, Value: *s.QbitCategory})
	}
	if s.MonitorSeasonPackPreference != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyMonitorSeasonPackPref, Value: string(*s.MonitorSeasonPackPreference)})
	}
	if s.WorkerMonitorInterval != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyWorkerMonitorInterval, Value: strconv.Itoa(*s.WorkerMonitorInterval)})
	}
	if s.WorkerDownloadInterval != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyWorkerDownloadInterval, Value: strconv.Itoa(*s.WorkerDownloadInterval)})
	}
	if s.WorkerImporterInterval != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyWorkerImporterInterval, Value: strconv.Itoa(*s.WorkerImporterInterval)})
	}
	if s.WorkerMetadataRefreshInterval != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyWorkerMetadataRefreshInterval, Value: strconv.Itoa(*s.WorkerMetadataRefreshInterval)})
	}
	if s.WorkerUpdateCheckInterval != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyWorkerUpdateCheckInterval, Value: strconv.Itoa(*s.WorkerUpdateCheckInterval)})
	}
	if s.LibraryBasePath != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyLibraryBasePath, Value: *s.LibraryBasePath})
	}
	if s.OnboardingStep != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOnboardingStep, Value: strconv.Itoa(*s.OnboardingStep)})
	}
	if s.OnboardingCompleted != nil {
		val := "false"
		if *s.OnboardingCompleted {
			val = "true"
		}
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOnboardingCompleted, Value: val})
	}
	if s.FlaresolverrUrl != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyFlareSolverrURL, Value: *s.FlaresolverrUrl})
	}
	if s.DiscordWebhookUrl != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyDiscordWebhookURL, Value: *s.DiscordWebhookUrl})
	}
	if s.GlobalExcludeTags != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyGlobalExcludeTags, Value: marshalJSON(*s.GlobalExcludeTags)})
	}
	if s.WatchedListMode != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyWatchedListMode, Value: string(*s.WatchedListMode)})
	}
	if s.OpensubtitlesApiKey != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOpenSubtitlesApiKey, Value: *s.OpensubtitlesApiKey})
	}
	if s.OpensubtitlesUsername != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOpenSubtitlesUsername, Value: *s.OpensubtitlesUsername})
	}
	if s.OpensubtitlesPassword != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOpenSubtitlesPassword, Value: *s.OpensubtitlesPassword})
	}
	if s.OpensubtitlesRateLimit != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOpenSubtitlesRateLimit, Value: strconv.Itoa(*s.OpensubtitlesRateLimit)})
	}
	if s.SubtitleLanguages != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeySubtitleLanguages, Value: marshalJSON(*s.SubtitleLanguages)})
	}
	if s.SubtitleAutoSearch != nil {
		val := "false"
		if *s.SubtitleAutoSearch {
			val = "true"
		}
		kvs = append(kvs, settings.KeyValue{Key: settings.KeySubtitleAutoSearch, Value: val})
	}
	if s.OtelEnabled != nil {
		val := "false"
		if *s.OtelEnabled {
			val = "true"
		}
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOTelEnabled, Value: val})
	}
	if s.OtelEndpoint != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOTelEndpoint, Value: *s.OtelEndpoint})
	}
	if s.OtelService != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOTelService, Value: *s.OtelService})
	}
	if s.OtelLogLevel != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyOTelLogLevel, Value: string(*s.OtelLogLevel)})
	}
	if s.PlexUrl != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyPlexURL, Value: *s.PlexUrl})
	}
	if s.PlexToken != nil {
		kvs = append(kvs, settings.KeyValue{Key: settings.KeyPlexToken, Value: *s.PlexToken})
	}
	return kvs
}

func mediaFileToAPI(f *store.MediaFile) MediaFile {
	api := MediaFile{
		Id:          int64(f.ID),
		MediaItemId: int64(f.MediaItemID),
		Path:        f.Path,
		FileName:    f.FileName,
		AddedAt:     f.AddedAt,
	}
	if f.Size > 0 {
		api.Size = &f.Size
	}
	if f.Resolution != "" {
		api.Resolution = &f.Resolution
	}
	if f.SourceType != "" {
		api.SourceType = &f.SourceType
	}
	if f.SeasonNumber != nil {
		api.SeasonNumber = f.SeasonNumber
	}
	if f.EpisodeNumber != nil {
		api.EpisodeNumber = f.EpisodeNumber
	}
	return api
}

func candidateToAPI(c matching.Candidate) MatchCandidate {
	mc := MatchCandidate{
		Source:     MatchCandidateSource(c.Source),
		ExternalId: c.ExternalID,
		Title:      c.Title,
		Confidence: float32(c.Confidence),
	}
	if c.Overview != "" {
		mc.Overview = &c.Overview
	}
	if c.Year != nil {
		mc.Year = c.Year
	}
	if c.PosterURL != "" {
		mc.PosterUrl = &c.PosterURL
	}
	if c.ExistingMediaID != nil {
		id := int64(*c.ExistingMediaID)
		mc.ExistingMediaId = &id
	}
	return mc
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func scanEntriesToAPI(entries []library.ScanEntry) []ScanEntry {
	result := make([]ScanEntry, len(entries))
	for i, e := range entries {
		result[i] = ScanEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDirectory,
			Size:        e.Size,
			ModifiedAt:  e.ModifiedAt,
		}
	}
	return result
}

func candidatesToAPI(candidates []matching.Candidate) []MatchCandidate {
	result := make([]MatchCandidate, len(candidates))
	for i, c := range candidates {
		result[i] = candidateToAPI(c)
	}
	return result
}

func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func applyProfileFields(p *store.MediaProfile, name string, resolutions, languages []string, languageMode *MediaProfileCreateLanguageMode, sources, excludeTags *[]string) {
	p.Name = name
	p.Resolutions = marshalJSON(resolutions)
	p.Languages = marshalJSON(languages)
	if languageMode != nil {
		p.LanguageMode = string(*languageMode)
	} else {
		p.LanguageMode = "or"
	}
	if sources != nil {
		p.Sources = marshalJSON(*sources)
	} else {
		p.Sources = ""
	}
	if excludeTags != nil {
		p.ExcludeTags = marshalJSON(*excludeTags)
	} else {
		p.ExcludeTags = ""
	}
}

func definitionToAPI(d *indexer.DefinitionInfo) IndexerDefinition {
	api := IndexerDefinition{
		Id:       d.ID,
		Name:     d.Name,
		Type:     d.Type,
		Language: d.Language,
	}
	if d.Description != "" {
		api.Description = &d.Description
	}
	if len(d.Links) > 0 {
		api.Links = &d.Links
	}
	if len(d.Settings) > 0 {
		settings := make([]IndexerDefinitionSetting, len(d.Settings))
		for i, s := range d.Settings {
			settings[i] = IndexerDefinitionSetting{
				Name:  s.Name,
				Type:  s.Type,
				Label: s.Label,
			}
			if s.Default != "" {
				settings[i].Default = &s.Default
			}
		}
		api.Settings = &settings
	}
	return api
}

func indexerInfoToAPI(info *indexer.IndexerInfo) Indexer {
	api := Indexer{
		Id:           int64(info.ID),
		Name:         info.Name,
		DefinitionId: info.DefinitionID,
		Enabled:      info.Enabled,
		Priority:     info.Priority,
	}
	if len(info.Settings) > 0 {
		s := info.Settings
		api.Settings = &s
	}
	if info.SeedMinRatio != 0 {
		r := float32(info.SeedMinRatio)
		api.SeedMinRatio = &r
	}
	if info.SeedMinTime != 0 {
		api.SeedMinTime = &info.SeedMinTime
	}
	return api
}

func torrentResultToAPI(r *indexer.TorrentResult) TorrentResult {
	api := TorrentResult{
		IndexerId:   int64(r.IndexerID),
		IndexerName: r.IndexerName,
		Title:       r.Title,
		Size:        r.Size,
		Seeders:     r.Seeders,
		Leechers:    r.Leechers,
		Date:        r.Date,
	}
	if r.DetailsURL != "" {
		api.DetailsUrl = &r.DetailsURL
	}
	if r.DownloadURL != "" {
		api.DownloadUrl = &r.DownloadURL
	}
	if r.Category != "" {
		api.Category = &r.Category
	}
	if r.CategoryDesc != "" {
		api.CategoryDesc = &r.CategoryDesc
	}
	if r.ImdbID != "" {
		api.ImdbId = &r.ImdbID
	}
	if r.DownloadVolumeFactor != 1.0 {
		dvf := float32(r.DownloadVolumeFactor)
		api.DownloadVolumeFactor = &dvf
	}
	if r.UploadVolumeFactor != 1.0 {
		uvf := float32(r.UploadVolumeFactor)
		api.UploadVolumeFactor = &uvf
	}
	return api
}

func watchedItemToAPI(item *store.WatchedItem) WatchedItem {
	api := WatchedItem{
		Id:        int64(item.ID),
		Source:    WatchedItemSource(item.Source),
		ExternalId: item.ExternalID,
		Title:     item.Title,
		MediaType: WatchedItemMediaType(item.MediaType),
		WatchedAt: item.WatchedAt,
	}
	if item.ImdbID != "" {
		api.ImdbId = &item.ImdbID
	}
	if item.Year != nil {
		api.Year = item.Year
	}
	if item.PosterPath != "" {
		api.PosterPath = &item.PosterPath
	}
	if item.MediaItemID != nil {
		id := int64(*item.MediaItemID)
		api.MediaItemId = &id
	}
	return api
}

func downloadToAPI(dl *store.Download) Download {
	api := Download{
		Id:              int64(dl.ID),
		MediaItemId:     int64(dl.MediaItemID),
		IndexerId:       int64(dl.IndexerID),
		IndexerName:     dl.IndexerName,
		Title:           dl.Title,
		DownloadUrl:     dl.DownloadURL,
		Status:          DownloadStatus(dl.Status),
		SeedingRequired: dl.SeedingRequired,
		LinkedToLibrary: dl.LinkedToLibrary,
		CreatedAt:       dl.CreatedAt,
		UpdatedAt:       dl.UpdatedAt,
	}
	if dl.EpisodeID != nil {
		eid := int64(*dl.EpisodeID)
		api.EpisodeId = &eid
	}
	if dl.SeasonNumber != nil {
		api.SeasonNumber = dl.SeasonNumber
	}
	if dl.DetailsURL != "" {
		api.DetailsUrl = &dl.DetailsURL
	}
	if dl.Size != "" {
		api.Size = &dl.Size
	}
	if dl.ImdbID != "" {
		api.ImdbId = &dl.ImdbID
	}
	if dl.ClientTorrentHash != "" {
		api.ClientTorrentHash = &dl.ClientTorrentHash
	}
	if dl.SavePath != "" {
		api.SavePath = &dl.SavePath
	}
	if dl.CompletedAt != nil {
		api.CompletedAt = dl.CompletedAt
	}
	if dl.RetryCount > 0 {
		api.RetryCount = &dl.RetryCount
	}
	if dl.NextRetryAt != nil {
		api.NextRetryAt = dl.NextRetryAt
	}
	if dl.LastError != "" {
		api.LastError = &dl.LastError
	}
	if dl.MediaItemTitle != "" {
		api.MediaItemTitle = &dl.MediaItemTitle
	}
	return api
}
