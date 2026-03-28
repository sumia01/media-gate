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

type MediaItem struct {
	ID         uint   `gorm:"primarykey"`
	LibraryID  uint   `gorm:"not null;index"`
	Title      string `gorm:"not null"`
	FolderName string `gorm:"not null"`
	Path       string `gorm:"not null;uniqueIndex"`
	MediaType  string `gorm:"not null"`
	Status     string `gorm:"not null;default:new"`
	Year       *int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Setting struct {
	Key       string `gorm:"primarykey"`
	Value     string `gorm:"not null"`
	Sensitive bool   `gorm:"not null;default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
