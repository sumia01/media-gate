package auth

import (
	"crypto/rand"
	"crypto/sha256"
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
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
)

func validatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}
	return nil
}

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
	tickets            *TicketStore
}

func NewService(s store.Store, secretKey string) *Service {
	return &Service{
		store:              s,
		jwtSecret:          crypto.DeriveKey(secretKey),
		accessTTL:          15 * time.Minute,
		refreshTTL:         24 * time.Hour,
		refreshTTLRemember: 30 * 24 * time.Hour,
		tickets:            NewTicketStore(30 * time.Second),
	}
}

// IssueSSETicket creates a short-lived single-use ticket for SSE authentication.
func (s *Service) IssueSSETicket(userID uint) (string, error) {
	return s.tickets.Issue(userID)
}

// RedeemSSETicket validates and consumes an SSE ticket.
func (s *Service) RedeemSSETicket(ticket string) (uint, error) {
	return s.tickets.Redeem(ticket)
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

// hashToken returns the hex-encoded SHA-256 hash of a token.
// Refresh tokens are stored hashed for defense in depth.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Service) GenerateRefreshToken(userID uint, rememberMe bool) (*store.RefreshToken, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generating random token: %w", err)
	}

	ttl := s.refreshTTL
	if rememberMe {
		ttl = s.refreshTTLRemember
	}

	raw := hex.EncodeToString(b)
	rt := &store.RefreshToken{
		UserID:    userID,
		Token:     hashToken(raw),
		ExpiresAt: time.Now().Add(ttl),
	}
	if err := s.store.CreateRefreshToken(rt); err != nil {
		return nil, err
	}
	// Return plaintext token for the cookie — DB stores only the hash.
	rt.Token = raw
	return rt, nil
}

func (s *Service) RotateRefreshToken(oldToken string) (*store.RefreshToken, *store.User, error) {
	hashed := hashToken(oldToken)
	rt, err := s.store.GetRefreshTokenByToken(hashed)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if time.Now().After(rt.ExpiresAt) {
		_ = s.store.DeleteRefreshToken(hashed)
		return nil, nil, ErrInvalidCredentials
	}

	user, err := s.store.GetUser(rt.UserID)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	_ = s.store.DeleteRefreshToken(hashed)

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
	return s.store.DeleteRefreshToken(hashToken(token))
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
	if err := validatePassword(password); err != nil {
		return nil, err
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
	if err := validatePassword(newPassword); err != nil {
		return err
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
// Returns nil if users already exist or if no credentials are provided
// (the setup wizard will handle first-user creation in that case).
func (s *Service) Bootstrap(defaultEmail, defaultPassword string) error {
	count, err := s.store.CountUsers()
	if err != nil {
		return fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return nil
	}

	if defaultEmail == "" || defaultPassword == "" {
		slog.Info("no users in database and no default user credentials — setup wizard required")
		return nil
	}

	user, err := s.Register(defaultEmail, defaultPassword, "", "", nil)
	if err != nil {
		return fmt.Errorf("creating default user: %w", err)
	}
	slog.Info("created default user from environment", "email", user.Email)
	return nil
}

// CountUsers returns the number of registered users.
func (s *Service) CountUsers() (int64, error) {
	return s.store.CountUsers()
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
