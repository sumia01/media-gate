package store

import "io"

// Store defines the data access interface.
// Implementations must be safe for concurrent use.
type Store interface {
	io.Closer
	// Ping verifies the database connection is alive.
	Ping() error
}
