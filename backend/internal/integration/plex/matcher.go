package plex

import (
	"path/filepath"
	"strings"
)

// MatchResult holds the outcome of auto-matching a library to a Plex section.
type MatchResult struct {
	SectionID    string
	SectionTitle string
	Score        int
}

// AutoMatch attempts to find the best Plex section for a given library.
// It returns the best match (or nil if no match scores above threshold).
// Scoring: type match = 10, basename match = +5, full path match = +20.
func AutoMatch(libraryPath, libraryType string, sections []Section) *MatchResult {
	const minScore = 10

	var best *MatchResult
	cleanPath := filepath.Clean(libraryPath)
	baseName := strings.ToLower(filepath.Base(cleanPath))

	for _, s := range sections {
		if !typesCompatible(libraryType, s.Type) {
			continue
		}
		score := 10 // type match baseline

		for _, loc := range s.Locations {
			cleanLoc := filepath.Clean(loc)
			if cleanLoc == cleanPath {
				score += 20
				break
			}
			if strings.ToLower(filepath.Base(cleanLoc)) == baseName {
				score += 5
			}
		}

		if best == nil || score > best.Score {
			best = &MatchResult{
				SectionID:    s.ID,
				SectionTitle: s.Title,
				Score:        score,
			}
		}
	}

	if best != nil && best.Score >= minScore {
		return best
	}
	return nil
}

// FindSection returns the section with the given ID, or nil.
func FindSection(sections []Section, id string) *Section {
	for i := range sections {
		if sections[i].ID == id {
			return &sections[i]
		}
	}
	return nil
}

// typesCompatible checks if a Media Gate library type matches a Plex section type.
func typesCompatible(libraryType, sectionType string) bool {
	switch libraryType {
	case "movie":
		return sectionType == "movie"
	case "tv":
		return sectionType == "show"
	default:
		return false
	}
}
