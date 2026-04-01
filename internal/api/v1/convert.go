package apiv1

import (
	"encoding/json"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/store"
)

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
	apiItem.MonitorSearchStartedAt = item.MonitorSearchStartedAt
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

func settingToAPI(s *store.Setting) Setting {
	return Setting{
		Key:       s.Key,
		Value:     s.Value,
		Sensitive: &s.Sensitive,
	}
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

func mediaProfileFromAPI(name string, resolutions, languages []string, sources, excludeTags *[]string) *store.MediaProfile {
	p := &store.MediaProfile{
		Name:        name,
		Resolutions: marshalJSON(resolutions),
		Languages:   marshalJSON(languages),
	}
	if sources != nil {
		p.Sources = marshalJSON(*sources)
	}
	if excludeTags != nil {
		p.ExcludeTags = marshalJSON(*excludeTags)
	}
	return p
}

func updateMediaProfileFromAPI(p *store.MediaProfile, name string, resolutions, languages []string, sources, excludeTags *[]string) {
	p.Name = name
	p.Resolutions = marshalJSON(resolutions)
	p.Languages = marshalJSON(languages)
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
	return api
}
