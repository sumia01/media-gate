package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend build output as a filesystem
// rooted at the dist/ directory contents.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
