package sqlite

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sumia01/media-gate/internal/fileparse"
	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm/clause"
)

func (s *SQLiteStore) RememberReleaseKeywords(userID uint, keywords []string) error {
	rows := make([]store.ReleaseKeyword, 0, len(keywords))
	for _, keyword := range fileparse.ParseReleaseKeywords(strings.Join(keywords, ",")) {
		// Long free-form filters remain valid, but are not autocomplete names.
		if utf8.RuneCountInString(keyword) <= store.MaxReleaseKeywordLength {
			rows = append(rows, store.ReleaseKeyword{UserID: userID, Keyword: keyword})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 100).Error
}

func (s *SQLiteStore) ListReleaseKeywords(userID uint, prefix string) ([]string, error) {
	keywords := make([]string, 0)
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" || utf8.RuneCountInString(prefix) > store.MaxReleaseKeywordLength {
		return keywords, nil
	}
	// Stream only this user's vocabulary and retain a bounded best-match list.
	// Applying a SQL limit first would silently miss non-prefix/fuzzy matches.
	rows, err := s.db.Model(&store.ReleaseKeyword{}).Select("keyword").Where("user_id = ?", userID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type match struct {
		keyword string
		score   int
	}
	matches := make([]match, 0, store.MaxReleaseKeywordSuggestions+1)
	for rows.Next() {
		var keyword string
		if err := rows.Scan(&keyword); err != nil {
			return nil, err
		}
		score := fileparse.ReleaseSuggestionScore(keyword, prefix)
		if score < 0 {
			continue
		}
		index := sort.Search(len(matches), func(i int) bool {
			return matches[i].score > score || matches[i].score == score && matches[i].keyword > keyword
		})
		if index >= store.MaxReleaseKeywordSuggestions {
			continue
		}
		matches = append(matches, match{})
		copy(matches[index+1:], matches[index:])
		matches[index] = match{keyword: keyword, score: score}
		if len(matches) > store.MaxReleaseKeywordSuggestions {
			matches = matches[:store.MaxReleaseKeywordSuggestions]
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, match := range matches {
		keywords = append(keywords, match.keyword)
	}
	return keywords, nil
}
