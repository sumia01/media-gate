package store

import (
	"errors"
	"io"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("record not found")

// Store defines the data access interface.
// Implementations must be safe for concurrent use.
type Store interface {
	io.Closer
	// Ping verifies the database connection is alive.
	Ping() error

	CreateLibrary(lib *Library) error
	ListLibraries() ([]Library, error)
	GetLibrary(id uint) (*Library, error)
	UpdateLibrary(lib *Library) error
	DeleteLibrary(id uint) error
}
