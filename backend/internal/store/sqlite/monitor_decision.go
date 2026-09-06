package sqlite

import (
	"errors"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *SQLiteStore) GetMonitorDecision(mediaItemID uint) (*store.MonitorDecision, error) {
	var decision store.MonitorDecision
	err := s.db.Where("media_item_id = ?", mediaItemID).First(&decision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &decision, nil
}

func (s *SQLiteStore) UpsertMonitorDecision(decision *store.MonitorDecision) error {
	if decision.Details == nil {
		decision.Details = []store.MonitorDecisionDetail{}
	}
	return s.db.Omit(clause.Associations).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "media_item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"checked_at", "input_updated_at", "outcome", "summary", "details", "truncated",
		}),
	}).Create(decision).Error
}
