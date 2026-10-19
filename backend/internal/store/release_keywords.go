package store

const (
	MaxReleaseKeywordLength      = 256
	MaxReleaseKeywordSuggestions = 10
)

// ReleaseKeyword is a user's previously saved keyword, independent of the
// media item it was used for. User deletion removes this private history.
type ReleaseKeyword struct {
	UserID  uint   `gorm:"primaryKey"`
	Keyword string `gorm:"primaryKey"`
}
