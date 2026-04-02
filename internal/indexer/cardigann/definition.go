package cardigann

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Definition represents a complete Cardigann indexer definition.
type Definition struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Language    string   `yaml:"language"`
	Type        string   `yaml:"type"` // private, public, semi-private
	Encoding    string   `yaml:"encoding"`
	Links       []string `yaml:"links"`
	LegacyLinks []string `yaml:"legacylinks"`

	Caps     Caps           `yaml:"caps"`
	Settings []SettingField `yaml:"settings"`
	Login    Login          `yaml:"login"`
	Download DownloadBlock  `yaml:"download"`
	Search   Search         `yaml:"search"`
}

// Caps describes the indexer's capabilities.
type Caps struct {
	CategoryMappings []CategoryMapping  `yaml:"categorymappings"`
	Modes            map[string][]string `yaml:"modes"`
	AllowRawSearch   bool               `yaml:"allowrawsearch"`
}

// CategoryMapping maps a site-specific category ID to a Newznab standard category.
type CategoryMapping struct {
	ID   string `yaml:"id"`
	Cat  string `yaml:"cat"`
	Desc string `yaml:"desc"`
}

// SettingField describes a user-configurable setting for an indexer.
type SettingField struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"` // text, password, info, checkbox
	Label   string `yaml:"label"`
	Default string `yaml:"default"`
}

// Login describes how to authenticate with the indexer.
type Login struct {
	Method string            `yaml:"method"` // post, form, cookie, get
	Path   string            `yaml:"path"`
	Inputs map[string]string `yaml:"inputs"`
	Error  []ErrorBlock      `yaml:"error"`
	Test   TestBlock         `yaml:"test"`
}

// ErrorBlock identifies an error on a login response page.
type ErrorBlock struct {
	Selector string       `yaml:"selector"`
	Message  StringOrText `yaml:"message"`
}

// StringOrText accepts either a plain string or an object with a "text" field.
// Many Prowlarr v11 definitions use message: {text: "..."} instead of a bare string.
type StringOrText string

func (s *StringOrText) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*s = StringOrText(value.Value)
		return nil
	}
	var m struct {
		Text string `yaml:"text"`
	}
	if err := value.Decode(&m); err != nil {
		return err
	}
	*s = StringOrText(m.Text)
	return nil
}

// TestBlock verifies a login was successful.
type TestBlock struct {
	Path     string `yaml:"path"`
	Selector string `yaml:"selector"`
}

// DownloadBlock describes how to extract download links.
type DownloadBlock struct {
	Selectors []DownloadSelector `yaml:"selectors"`
}

// DownloadSelector specifies where to find a download link.
type DownloadSelector struct {
	Selector  string `yaml:"selector"`
	Attribute string `yaml:"attribute"`
}

// Search describes how to perform searches and parse results.
type Search struct {
	Paths  []SearchPath      `yaml:"paths"`
	Inputs map[string]string `yaml:"inputs"`
	Rows   RowsBlock         `yaml:"rows"`
	Fields map[string]FieldDef `yaml:"fields"`
}

// SearchPath is a URL path used for searching.
type SearchPath struct {
	Path       string   `yaml:"path"`
	Categories []string `yaml:"categories"`
}

// RowsBlock specifies how to find result rows in the response.
type RowsBlock struct {
	Selector string `yaml:"selector"`
}

// UnmarshalYAML handles RowsBlock being either a plain string or an object.
func (r *RowsBlock) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		r.Selector = value.Value
		return nil
	}
	type raw RowsBlock
	return value.Decode((*raw)(r))
}

// FieldDef describes how to extract a single field from a result row.
type FieldDef struct {
	Selector  string            `yaml:"selector"`
	Attribute string            `yaml:"attribute"`
	Optional  bool              `yaml:"optional"`
	Text      string            `yaml:"text"`
	Filters   []Filter          `yaml:"filters"`
	Case      map[string]string `yaml:"case"`
}

// UnmarshalYAML handles FieldDef being either a plain string selector or an object.
func (f *FieldDef) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		f.Selector = value.Value
		return nil
	}
	type raw FieldDef
	return value.Decode((*raw)(f))
}

// Filter transforms an extracted field value.
type Filter struct {
	Name string   `yaml:"name"`
	Args []string `yaml:"-"`
}

// UnmarshalYAML normalises filter args to a string slice.
func (f *Filter) UnmarshalYAML(value *yaml.Node) error {
	var m struct {
		Name string   `yaml:"name"`
		Args yaml.Node `yaml:"args"`
	}
	if err := value.Decode(&m); err != nil {
		return err
	}
	f.Name = m.Name

	switch m.Args.Kind {
	case 0:
		// no args
	case yaml.ScalarNode:
		f.Args = []string{m.Args.Value}
	case yaml.SequenceNode:
		for _, n := range m.Args.Content {
			f.Args = append(f.Args, n.Value)
		}
	default:
		return fmt.Errorf("unsupported args type for filter %q", f.Name)
	}
	return nil
}

// ParseDefinition parses a Cardigann YAML definition from raw bytes.
func ParseDefinition(data []byte) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parsing definition: %w", err)
	}
	if def.ID == "" {
		return nil, fmt.Errorf("definition missing required field: id")
	}
	return &def, nil
}
