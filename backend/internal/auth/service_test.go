package auth

import (
	"errors"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
)

// fakeUserStore embeds store.Store so tests only need to implement the
// methods ChangePassword actually calls; any other call nil-panics and
// surfaces the gap rather than silently no-op'ing.
type fakeUserStore struct {
	store.Store
	users []*store.User

	revokedForUser  []uint
	deleteTokensErr error
}

func (f *fakeUserStore) GetUser(id uint) (*store.User, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeUserStore) UpdateUser(u *store.User) error {
	for i, existing := range f.users {
		if existing.ID == u.ID {
			f.users[i] = u
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeUserStore) DeleteRefreshTokensByUser(userID uint) error {
	f.revokedForUser = append(f.revokedForUser, userID)
	return f.deleteTokensErr
}

func newTestService(t *testing.T, fs *fakeUserStore) *Service {
	t.Helper()
	return NewService(fs, "test-secret-key")
}

// TestChangePassword_RevokesRefreshTokens is the regression test for bug
// #14: a successful password change must invalidate all of the user's
// existing refresh tokens, otherwise a previously-issued (up to 30-day,
// remember-me) refresh cookie held by an attacker keeps working after the
// legitimate user changes their password specifically to lock them out.
func TestChangePassword_RevokesRefreshTokens(t *testing.T) {
	hash, err := hashPassword("old-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	fs := &fakeUserStore{
		users: []*store.User{{ID: 1, Email: "user@example.com", PasswordHash: hash}},
	}
	svc := newTestService(t, fs)

	if err := svc.ChangePassword(1, "old-password", "new-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if len(fs.revokedForUser) != 1 || fs.revokedForUser[0] != 1 {
		t.Fatalf("expected DeleteRefreshTokensByUser(1) to be called exactly once, got %v", fs.revokedForUser)
	}

	updated, err := fs.GetUser(1)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if err := checkPassword(updated.PasswordHash, "new-password"); err != nil {
		t.Fatalf("password hash was not updated to the new password: %v", err)
	}
}

// TestChangePassword_WrongOldPasswordDoesNotRevokeTokens ensures a failed
// (unauthenticated) change-password attempt never triggers token
// revocation — that would let an attacker who merely guesses at the
// endpoint (without knowing the old password) log the legitimate user out
// everywhere.
func TestChangePassword_WrongOldPasswordDoesNotRevokeTokens(t *testing.T) {
	hash, err := hashPassword("old-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	fs := &fakeUserStore{
		users: []*store.User{{ID: 1, Email: "user@example.com", PasswordHash: hash}},
	}
	svc := newTestService(t, fs)

	err = svc.ChangePassword(1, "wrong-password", "new-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if len(fs.revokedForUser) != 0 {
		t.Fatalf("did not expect any refresh token revocation, got %v", fs.revokedForUser)
	}
}

// TestChangePassword_RevocationErrorIsSurfaced ensures that if revoking refresh
// tokens fails (e.g. transient DB error), ChangePassword returns an error — the
// security intent of the call (locking out sessions opened with the old
// credentials) was not met, so it must not report silent success. The password
// change itself was already persisted; that side effect is verified too.
func TestChangePassword_RevocationErrorIsSurfaced(t *testing.T) {
	hash, err := hashPassword("old-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	fs := &fakeUserStore{
		users:           []*store.User{{ID: 1, Email: "user@example.com", PasswordHash: hash}},
		deleteTokensErr: errors.New("db unavailable"),
	}
	svc := newTestService(t, fs)

	if err := svc.ChangePassword(1, "old-password", "new-password"); err == nil {
		t.Fatal("ChangePassword should return an error when token revocation fails")
	}
	// The password change must still have been persisted despite the error.
	if checkPassword(fs.users[0].PasswordHash, "new-password") != nil {
		t.Fatal("password should have been updated to the new value despite the revocation error")
	}
	// A revocation attempt must have been made.
	if len(fs.revokedForUser) != 1 {
		t.Fatalf("expected a revocation attempt to be recorded, got %v", fs.revokedForUser)
	}
}
