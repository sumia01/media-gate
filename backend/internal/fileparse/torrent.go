package fileparse

import (
	"regexp"
	"strconv"
)

// TorrentSeasonEpisode holds parsed season/episode info from a torrent title.
type TorrentSeasonEpisode struct {
	Season     *int // nil if no season detected
	Episode    *int // nil if no episode (e.g. season pack)
	EpisodeEnd *int // non-nil for episode ranges like S01E01-E10
}

var (
	// S02E01-E10 (episode range) and S02E01-10 (range without a second "E").
	// The trailing \b prevents the range-end digits from bleeding into an
	// adjacent quality/resolution/year token (e.g. "S01E01-1080p" or
	// "S01E01-1984"): those are contiguous word-char runs (digits followed by
	// a letter, or a longer digit run) with no internal boundary, so \d{1,3}
	// can't stop short and still satisfy \b.
	torrentSxExRangeRe = regexp.MustCompile(`(?i)S(\d{1,2})E(\d{1,3})-E?(\d{1,3})\b`)
	// S02E05 (standard single episode)
	torrentSxExRe = regexp.MustCompile(`(?i)S(\d{1,2})E(\d{1,3})`)
	// Standalone S02 (word-boundary guarded)
	torrentSOnlyRe = regexp.MustCompile(`(?i)(?:^|[\s.\-_\[(])S(\d{1,2})(?:[\s.\-_\])]|$)`)
	// Season 2 Episode 5
	torrentSeasonEpRe = regexp.MustCompile(`(?i)Season[\s._-]*(\d{1,2})[\s._-]*Episode[\s._-]*(\d{1,3})`)
	// Season 2
	torrentSeasonOnlyRe = regexp.MustCompile(`(?i)Season[\s._-]*(\d{1,2})`)
	// 2x05
	torrentNxNRe = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
)

// ParseTorrentSeasonEpisode extracts season and episode info from a torrent title.
// It handles ranges (S01E01-E10), standard (S02E05), season-only (S02),
// spelled-out (Season 2 Episode 5), and cross-style (2x05).
func ParseTorrentSeasonEpisode(title string) TorrentSeasonEpisode {
	var result TorrentSeasonEpisode

	if m := torrentSxExRangeRe.FindStringSubmatch(title); m != nil {
		s, _ := strconv.Atoi(m[1])
		e1, _ := strconv.Atoi(m[2])
		e2, _ := strconv.Atoi(m[3])
		result.Season = &s
		result.Episode = &e1
		result.EpisodeEnd = &e2
		return result
	}

	if m := torrentSxExRe.FindStringSubmatch(title); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		result.Season = &s
		result.Episode = &e
		return result
	}

	if m := torrentSOnlyRe.FindStringSubmatch(title); m != nil {
		s, _ := strconv.Atoi(m[1])
		result.Season = &s
		return result
	}

	if m := torrentSeasonEpRe.FindStringSubmatch(title); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		result.Season = &s
		result.Episode = &e
		return result
	}

	if m := torrentSeasonOnlyRe.FindStringSubmatch(title); m != nil {
		s, _ := strconv.Atoi(m[1])
		result.Season = &s
		return result
	}

	if m := torrentNxNRe.FindStringSubmatch(title); m != nil {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		result.Season = &s
		result.Episode = &e
		return result
	}

	return result
}
