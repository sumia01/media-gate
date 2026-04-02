package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/sumia01/media-gate/internal/crypto"
	"github.com/sumia01/media-gate/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserExists         = errors.New("user with this email already exists")
)

// AccessClaims are the JWT payload fields.
type AccessClaims struct {
	jwt.RegisteredClaims
	UserID uint   `json:"uid"`
	Email  string `json:"email"`
}

type Service struct {
	store              store.Store
	jwtSecret          []byte
	accessTTL          time.Duration
	refreshTTL         time.Duration
	refreshTTLRemember time.Duration
}

func NewService(s store.Store, secretKey string) *Service {
	return &Service{
		store:              s,
		jwtSecret:          crypto.DeriveKey(secretKey),
		accessTTL:          15 * time.Minute,
		refreshTTL:         24 * time.Hour,
		refreshTTLRemember: 30 * 24 * time.Hour,
	}
}

// --- Password hashing ---

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

func checkPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// --- JWT ---

func (s *Service) GenerateAccessToken(user *store.User) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "media-gate",
			Subject:   strconv.FormatUint(uint64(user.ID), 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
		UserID: user.ID,
		Email:  user.Email,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *Service) ValidateAccessToken(tokenString string) (*AccessClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AccessClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*AccessClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// --- Refresh tokens ---

func (s *Service) GenerateRefreshToken(userID uint, rememberMe bool) (*store.RefreshToken, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generating random token: %w", err)
	}

	ttl := s.refreshTTL
	if rememberMe {
		ttl = s.refreshTTLRemember
	}

	rt := &store.RefreshToken{
		UserID:    userID,
		Token:     hex.EncodeToString(b),
		ExpiresAt: time.Now().Add(ttl),
	}
	if err := s.store.CreateRefreshToken(rt); err != nil {
		return nil, err
	}
	return rt, nil
}

func (s *Service) RotateRefreshToken(oldToken string) (*store.RefreshToken, *store.User, error) {
	rt, err := s.store.GetRefreshTokenByToken(oldToken)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if time.Now().After(rt.ExpiresAt) {
		_ = s.store.DeleteRefreshToken(oldToken)
		return nil, nil, ErrInvalidCredentials
	}

	user, err := s.store.GetUser(rt.UserID)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	_ = s.store.DeleteRefreshToken(oldToken)

	// Determine TTL from remaining time on old token (preserve remember-me choice).
	remaining := time.Until(rt.ExpiresAt)
	rememberMe := remaining > s.refreshTTL

	newRT, err := s.GenerateRefreshToken(user.ID, rememberMe)
	if err != nil {
		return nil, nil, err
	}
	return newRT, user, nil
}

func (s *Service) RevokeRefreshToken(token string) error {
	return s.store.DeleteRefreshToken(token)
}

func (s *Service) RevokeAllUserTokens(userID uint) error {
	return s.store.DeleteRefreshTokensByUser(userID)
}

func (s *Service) CleanupExpiredTokens() error {
	return s.store.DeleteExpiredRefreshTokens()
}

// RefreshTTL returns the TTL for a refresh token based on rememberMe.
func (s *Service) RefreshTTL(rememberMe bool) time.Duration {
	if rememberMe {
		return s.refreshTTLRemember
	}
	return s.refreshTTL
}

// --- User management ---

func (s *Service) Register(email, password, firstName, lastName string, birthYear *int) (*store.User, error) {
	if email == "" {
		return nil, errors.New("email is required")
	}
	if password == "" {
		return nil, errors.New("password is required")
	}

	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	user := &store.User{
		Email:        email,
		PasswordHash: hash,
		FirstName:    firstName,
		LastName:     lastName,
		BirthYear:    birthYear,
	}
	if err := s.store.CreateUser(user); err != nil {
		if isDuplicateEmail(err) {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return user, nil
}

func (s *Service) Authenticate(email, password string) (*store.User, error) {
	user, err := s.store.GetUserByEmail(email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if err := checkPassword(user.PasswordHash, password); err != nil {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

func (s *Service) ChangePassword(userID uint, oldPassword, newPassword string) error {
	user, err := s.store.GetUser(userID)
	if err != nil {
		return err
	}
	if err := checkPassword(user.PasswordHash, oldPassword); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	return s.store.UpdateUser(user)
}

func (s *Service) GetUser(id uint) (*store.User, error) {
	return s.store.GetUser(id)
}

func (s *Service) UpdateProfile(userID uint, firstName, lastName string, birthYear *int) (*store.User, error) {
	user, err := s.store.GetUser(userID)
	if err != nil {
		return nil, err
	}
	user.FirstName = firstName
	user.LastName = lastName
	user.BirthYear = birthYear
	if err := s.store.UpdateUser(user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) ListUsers() ([]store.User, error) {
	return s.store.ListUsers()
}

func (s *Service) DeleteUser(id uint) error {
	_ = s.store.DeleteRefreshTokensByUser(id)
	return s.store.DeleteUser(id)
}

// Bootstrap creates the initial user from env vars if no users exist.
// Returns nil if users already exist. Returns an error if no users exist and no credentials provided.
func (s *Service) Bootstrap(defaultEmail, defaultPassword string) error {
	count, err := s.store.CountUsers()
	if err != nil {
		return fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return nil
	}

	if defaultEmail == "" || defaultPassword == "" {
		return errors.New("no users in database and DEFAULTUSER_EMAIL/DEFAULTUSER_PASSWORD not set — cannot start without at least one user")
	}

	user, err := s.Register(defaultEmail, defaultPassword, "", "", nil)
	if err != nil {
		return fmt.Errorf("creating default user: %w", err)
	}
	slog.Info("created default user from environment", "email", user.Email)
	return nil
}

// isDuplicateEmail checks if a GORM error is a unique constraint violation on email.
func isDuplicateEmail(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "UNIQUE constraint failed") || contains(msg, "duplicate key")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
