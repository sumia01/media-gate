package cardigann

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// TemplateContext is the dot-context passed to Cardigann Go templates.
type TemplateContext struct {
	Config     map[string]string
	Keywords   string
	Query      SearchQuery
	Categories []string
	Result     map[string]string
	False      bool
}

// SearchQuery holds structured search parameters.
type SearchQuery struct {
	Type       string // search, tv-search, movie-search, music-search, book-search
	Q          string
	IMDBID     string
	Season     string
	Ep         string
	Year       string
	Genre      string
	Categories []string
}

// rewriteMapAccess converts .Config.key / .Result.key dot-access into
// (index .Config "key") calls so that keys starting with digits or
// containing special characters work with Go's text/template parser.
var mapAccessRe = regexp.MustCompile(`\.(Config|Result)\.([a-zA-Z0-9_]+)`)

func rewriteMapAccess(tmplStr string) string {
	return mapAccessRe.ReplaceAllString(tmplStr, `(index .$1 "$2")`)
}

// RenderTemplate executes a single Cardigann template string.
func RenderTemplate(tmplStr string, ctx *TemplateContext) (string, error) {
	funcMap := template.FuncMap{
		"join": strings.Join,
		"replace": func(old, new_, s string) string {
			return strings.ReplaceAll(s, old, new_)
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).Parse(rewriteMapAccess(tmplStr))
	if err != nil {
		return "", fmt.Errorf("parsing template %q: %w", tmplStr, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("executing template %q: %w", tmplStr, err)
	}
	return buf.String(), nil
}

// RenderInputs processes a map of Cardigann input templates.
// Entries that render to "$raw" key are handled specially: the value is appended raw.
func RenderInputs(inputs map[string]string, ctx *TemplateContext) (map[string]string, error) {
	result := make(map[string]string, len(inputs))
	for key, tmplStr := range inputs {
		val, err := RenderTemplate(tmplStr, ctx)
		if err != nil {
			return nil, fmt.Errorf("rendering input %q: %w", key, err)
		}
		result[key] = val
	}
	return result, nil
}
