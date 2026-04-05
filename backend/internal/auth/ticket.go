package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var ErrInvalidTicket = errors.New("invalid or expired ticket")

type sseTicket struct {
	userID    uint
	expiresAt time.Time
}

// TicketStore manages short-lived, single-use tickets for SSE authentication.
// Tickets replace JWT-in-URL: the client exchanges a JWT for a ticket via POST,
// then opens EventSource with ?ticket= instead of ?token=.
type TicketStore struct {
	mu      sync.Mutex
	tickets map[string]*sseTicket
	ttl     time.Duration
}

// NewTicketStore creates a ticket store with the given ticket TTL.
func NewTicketStore(ttl time.Duration) *TicketStore {
	return &TicketStore{
		tickets: make(map[string]*sseTicket),
		ttl:     ttl,
	}
}

// Issue creates a one-time ticket for the given user ID.
func (ts *TicketStore) Issue(userID uint) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(b)

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Lazy cleanup of expired tickets.
	now := time.Now()
	for k, t := range ts.tickets {
		if now.After(t.expiresAt) {
			delete(ts.tickets, k)
		}
	}

	ts.tickets[ticket] = &sseTicket{
		userID:    userID,
		expiresAt: now.Add(ts.ttl),
	}
	return ticket, nil
}

// Redeem validates and consumes a ticket, returning the associated user ID.
// The ticket is deleted on first use (single-use).
func (ts *TicketStore) Redeem(ticket string) (uint, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	t, ok := ts.tickets[ticket]
	if !ok {
		return 0, ErrInvalidTicket
	}
	delete(ts.tickets, ticket)

	if time.Now().After(t.expiresAt) {
		return 0, ErrInvalidTicket
	}
	return t.userID, nil
}
