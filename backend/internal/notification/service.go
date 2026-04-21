package notification

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/discord"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const colorGreen = 0x2ECC71

// Service subscribes to eventbus events and dispatches notifications.
type Service struct {
	store      store.Store
	settings   *settings.Service
	httpClient *http.Client
}

// NewService creates a notification service and subscribes to events on the bus.
// Must be called before bus.Start().
func NewService(db store.Store, settingsSvc *settings.Service, bus *eventbus.Bus, httpClient *http.Client) *Service {
	s := &Service{store: db, settings: settingsSvc, httpClient: httpClient}
	bus.Subscribe(eventbus.ImportCompleted, s.handleImportCompleted)
	return s
}

func (s *Service) handleImportCompleted(e eventbus.Event) {
	p, ok := e.Payload.(eventbus.ImportPayload)
	if !ok {
		return
	}

	webhookURL, err := s.settings.Get(settings.KeyDiscordWebhookURL)
	if err != nil || webhookURL == "" {
		return
	}

	dl, err := s.store.GetDownload(p.DownloadID)
	if err != nil {
		slog.Warn("discord: failed to get download", "error", err, "downloadId", p.DownloadID)
		return
	}

	item, _ := s.store.GetMediaItem(dl.MediaItemID)
	meta, _ := s.store.GetMediaMetadataByMediaItem(dl.MediaItemID)

	var lib *store.Library
	if item != nil {
		lib, _ = s.store.GetLibrary(item.LibraryID)
	}

	embed := s.buildImportEmbed(dl, item, meta, lib, p.FilesCount)

	client := discord.NewClient(webhookURL, s.httpClient)
	if err := client.Send(embed); err != nil {
		slog.Warn("discord notification failed", "error", err, "downloadId", p.DownloadID)
	}
}

func (s *Service) buildImportEmbed(dl *store.Download, item *store.MediaItem, meta *store.MediaMetadata, lib *store.Library, filesCount int) *discord.Embed {
	// Title: "Movie Name (2024)" or just "Movie Name"
	title := dl.Title
	if item != nil {
		title = item.Title
		if item.Year != nil {
			title = fmt.Sprintf("%s (%d)", item.Title, *item.Year)
		}
	}

	// Media type label
	mediaType := "Media Imported"
	if item != nil {
		switch item.MediaType {
		case "movie":
			mediaType = "Movie Imported"
		case "series":
			mediaType = "Series Imported"
		}
	}

	e := discord.NewEmbed().
		Author("MediaGate").
		Title(title).
		Color(colorGreen).
		Timestamp(time.Now())

	if meta != nil {
		if meta.Overview != "" {
			overview := meta.Overview
			if len(overview) > 300 {
				overview = overview[:297] + "..."
			}
			e.Description(overview)
		}

		if meta.PosterPath != "" {
			e.Thumbnail("https://image.tmdb.org/t/p/w500" + meta.PosterPath)
		}

		if meta.Rating != nil {
			e.Field("Rating", fmt.Sprintf("%.1f", *meta.Rating), true)
		}

		if meta.Genres != "" {
			e.Field("Genres", meta.Genres, true)
		}
	}

	e.Field("Quality", dl.Title, false)
	e.Field("Size", dl.Size, true)
	e.Field("Files", fmt.Sprintf("%d", filesCount), true)
	e.Field("Indexer", dl.IndexerName, true)

	if meta != nil {
		var links []string
		if meta.Source == "tmdb" {
			links = append(links, fmt.Sprintf("[TMDB](https://www.themoviedb.org/%s/%d)", tmdbMediaType(item), meta.ExternalID))
		}
		if meta.ImdbID != "" {
			links = append(links, fmt.Sprintf("[IMDb](https://www.imdb.com/title/%s)", meta.ImdbID))
		}
		if len(links) > 0 {
			e.Field("Links", strings.Join(links, " / "), false)
		}
	}

	// Footer: "Movie Imported to Movies" or "Movie Imported"
	footer := mediaType
	if lib != nil {
		footer = fmt.Sprintf("%s to %s", mediaType, lib.Name)
	}

	e.Footer(footer)

	return e
}

func tmdbMediaType(item *store.MediaItem) string {
	if item != nil && item.MediaType == "series" {
		return "tv"
	}
	return "movie"
}
