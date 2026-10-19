package sqlite

import (
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
	pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix) + "%"
	err := s.db.Model(&store.ReleaseKeyword{}).
		Where(`user_id = ? AND keyword LIKE ? ESCAPE '\'`, userID, pattern).
		Order("keyword ASC").Limit(store.MaxReleaseKeywordSuggestions).Pluck("keyword", &keywords).Error
	return keywords, err
}
