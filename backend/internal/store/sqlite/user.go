package sqlite

import (
	"errors"
	"time"

	"github.com/sumia01/media-gate/internal/store"
	"gorm.io/gorm"
)

// --- User ---

func (s *SQLiteStore) CreateUser(user *store.User) error {
	return s.db.Create(user).Error
}

func (s *SQLiteStore) GetUser(id uint) (*store.User, error) {
	return getByID[store.User](s.db, id)
}

func (s *SQLiteStore) GetUserByEmail(email string) (*store.User, error) {
	var user store.User
	if err := s.db.Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *SQLiteStore) ListUsers() ([]store.User, error) {
	var users []store.User
	if err := s.db.Order("email ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (s *SQLiteStore) UpdateUser(user *store.User) error {
	return save(s.db, user)
}

func (s *SQLiteStore) DeleteUser(id uint) error {
	return deleteByID(s.db, &store.User{}, id)
}

func (s *SQLiteStore) CountUsers() (int64, error) {
	var count int64
	if err := s.db.Model(&store.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// --- RefreshToken ---

func (s *SQLiteStore) CreateRefreshToken(token *store.RefreshToken) error {
	return s.db.Create(token).Error
}

func (s *SQLiteStore) GetRefreshTokenByToken(token string) (*store.RefreshToken, error) {
	var rt store.RefreshToken
	if err := s.db.Where("token = ?", token).First(&rt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &rt, nil
}

func (s *SQLiteStore) DeleteRefreshToken(token string) error {
	result := s.db.Where("token = ?", token).Delete(&store.RefreshToken{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (s *SQLiteStore) DeleteRefreshTokensByUser(userID uint) error {
	return s.db.Where("user_id = ?", userID).Delete(&store.RefreshToken{}).Error
}

func (s *SQLiteStore) DeleteExpiredRefreshTokens() error {
	return s.db.Where("expires_at < ?", time.Now()).Delete(&store.RefreshToken{}).Error
}
