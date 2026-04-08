package sqlite

import "github.com/sumia01/media-gate/internal/store"

func (s *SQLiteStore) CreateJobRecord(record *store.JobRecord) error {
	return s.db.Create(record).Error
}

func (s *SQLiteStore) ListJobRecords(limit int) ([]store.JobRecord, error) {
	var records []store.JobRecord
	if err := s.db.Order("completed_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (s *SQLiteStore) DeleteOldJobRecords(keep int) error {
	return s.db.Exec(
		"DELETE FROM job_records WHERE id NOT IN (SELECT id FROM job_records ORDER BY completed_at DESC LIMIT ?)",
		keep,
	).Error
}

func (s *SQLiteStore) MaxJobRecordID() (uint, error) {
	var maxID uint
	if err := s.db.Model(&store.JobRecord{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return 0, err
	}
	return maxID, nil
}
