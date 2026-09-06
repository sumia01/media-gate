package notification

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/integration/discord"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

const (
	colorGreen = 0x2ECC71
	colorRed   = 0xE74C3C
)

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
	bus.Subscribe(eventbus.DownloadFailed, s.handleFailure)
	bus.Subscribe(eventbus.ImportFailed, s.handleFailure)
	return s
}

func (s *Service) handleFailure(e eventbus.Event) {
	p, ok := e.Payload.(eventbus.DownloadPayload)
	if !ok {
		return
	}
	status, category := "failed", "Download Failed"
	if e.Type == eventbus.ImportFailed {
		status, category = "import_failed", "Import Failed"
	} else if e.Type != eventbus.DownloadFailed {
		return
	}

	webhookURL, err := s.settings.Get(settings.KeyDiscordWebhookURL)
	if err != nil || webhookURL == "" {
		return
	}
	dl, err := s.store.GetDownload(p.DownloadID)
	if err != nil {
		slog.Warn("discord: failed to get failed download", "error", err, "downloadId", p.DownloadID)
		return
	}
	if dl.Status != status {
		return // A delayed event must not notify after a retry or cancellation.
	}

	title := dl.Title
	if item, err := s.store.GetMediaItem(dl.MediaItemID); err == nil {
		title = item.Title
		if item.Year != nil {
			title = fmt.Sprintf("%s (%d)", title, *item.Year)
		}
	}
	embed := discord.NewEmbed().
		Author("MediaGate").
		Title(safeFailureTitle(title, dl.ID)).
		Color(colorRed).
		Field("Failure", category, true).
		Field("Reason", failureReason(dl), false).
		Footer(fmt.Sprintf("Download #%d", dl.ID)).
		Timestamp(e.Timestamp)
	// There is no configured public app URL; do not guess a link from service URLs.
	if err := discord.NewClient(webhookURL, s.httpClient).Send(embed); err != nil {
		slog.Warn("discord failure notification failed", "error", err, "downloadId", dl.ID)
	}
}

// Only fixed summaries leave the app. LastError can contain credentials, URLs,
// response bodies, or private filesystem paths, so never interpolate it.
func failureReason(dl *store.Download) string {
	reason := dl.LastError
	if dl.Status == "failed" {
		switch {
		case reason == "torrent missing from qBittorrent before import":
			return "Torrent was removed from qBittorrent before import."
		case reason == "qBittorrent reported missing files":
			return "qBittorrent reports missing download files."
		case reason == "qBittorrent reported a torrent error":
			return "qBittorrent reports a torrent error."
		case strings.HasPrefix(reason, "download retries exhausted:"):
			return "Could not fetch or add the torrent after all retry attempts."
		default:
			return "Download failed. Check MediaGate for details."
		}
	}
	switch {
	case strings.HasSuffix(reason, " (max import retries exceeded)"):
		return "Import could not finish after all retry attempts."
	case reason == "media item not found" || reason == "library not found":
		return "The media item or its library could not be found."
	case reason == "failed to create target directory" || reason == "failed to create release directory":
		return "Could not create the library import directory."
	case reason == "no torrent hash":
		return "The download has no torrent reference for import."
	case strings.HasPrefix(reason, "failed to hardlink/copy file:"):
		return "Could not link or copy a downloaded video into the library."
	case strings.HasPrefix(reason, "no video files imported ("):
		return "No usable video files were imported."
	default:
		return "Import failed. Check MediaGate for details."
	}
}

var sensitiveTitle = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://|magnet:|www\.|\b(api[_-]?key|pass(word|key)?|token|secret|authorization|cookie)\s*[:=])`)

func safeFailureTitle(title string, downloadID uint) string {
	title = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title))
	if title == "" || sensitiveTitle.MatchString(title) {
		return fmt.Sprintf("Download #%d", downloadID)
	}
	runes := []rune(title)
	if len(runes) > 256 {
		title = string(runes[:253]) + "..."
	}
	return title
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
