package store

import "time"

type Library struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"not null"`
	Path      string `gorm:"not null;uniqueIndex"`
	MediaType string `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
