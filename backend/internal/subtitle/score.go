package subtitle

import "strings"

// ScoreResult computes a match score for a subtitle search result.
// releaseName is the torrent release name from the Download record.
// languagePriorities is the user's ordered language preference list.
func ScoreResult(r *SearchResult, releaseName string, languagePriorities []string) int {
	score := 0

	// Hash match (strongest signal)
	if r.HashMatch {
		score += 500
	}

	// Release name matching
	if releaseName != "" && r.ReleaseName != "" {
		exact, partial := ReleaseMatch(r.ReleaseName, releaseName)
		if exact {
			score += 200
		} else if partial {
			score += 100
		}
	}

	// Language priority bonus (first language = 50, second = 40, ...)
	for i, lang := range languagePriorities {
		if strings.EqualFold(r.Language, lang) {
			bonus := 50 - (i * 10)
			if bonus < 0 {
				bonus = 0
			}
			score += bonus
			break
		}
	}

	// Trusted source bonus
	if r.Trusted {
		score += 50
	}

	// Download count bonus (capped at 30)
	dlBonus := r.DownloadCount / 1000
	if dlBonus > 30 {
		dlBonus = 30
	}
	score += dlBonus

	// Penalties
	if r.HearingImpaired {
		score -= 50
	}
	if r.ForeignPartsOnly {
		score -= 100
	}

	return score
}
