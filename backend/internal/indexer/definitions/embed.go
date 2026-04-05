package definitions

import (
	"embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed *.yml
var builtinFS embed.FS

// LoadBuiltin reads all embedded YAML definitions and returns them keyed by ID.
func LoadBuiltin() (map[string][]byte, error) {
	entries, err := builtinFS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("reading embedded definitions: %w", err)
	}

	defs := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := builtinFS.ReadFile(e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}

		var header struct {
			ID string `yaml:"id"`
		}
		if err := yaml.Unmarshal(data, &header); err != nil {
			return nil, fmt.Errorf("parsing id from %s: %w", e.Name(), err)
		}
		if header.ID == "" {
			return nil, fmt.Errorf("%s: missing required field: id", e.Name())
		}
		defs[header.ID] = data
	}
	return defs, nil
}
