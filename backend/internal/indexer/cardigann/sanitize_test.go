package cardigann

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSanitizeYAML_InvalidEscapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "arabtorrents backslash-d and backslash-slash",
			input: "id: arabtorrents\nargs: \"torrent-category-(\\d+)\\/\"\n",
		},
		{
			name:  "btarg backslash-slash and backslash-d",
			input: "id: btarg\nargs: \"^(\\d+) \\/\"\n",
		},
		{
			name:  "swarmazon-api backslash-slash",
			input: "id: swarmazon-api\nargs: [\"N\\/A\", \"\"]\n",
		},
		{
			name:  "already-escaped backslash stays intact",
			input: "id: test\nargs: \"already\\\\escaped\"\n",
		},
		{
			name:  "valid escapes unchanged",
			input: "id: test\nargs: \"newline\\ntab\\tquote\\\"\"\n",
		},
		{
			name:  "no double-quoted strings",
			input: "id: test\nargs: 'single quoted \\d'\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := SanitizeYAML([]byte(tt.input))

			// The sanitized output must be parseable by yaml.v3.
			var result map[string]interface{}
			if err := yaml.Unmarshal(sanitized, &result); err != nil {
				t.Errorf("SanitizeYAML output is not valid YAML:\ninput:     %q\nsanitized: %q\nerror:     %v",
					tt.input, string(sanitized), err)
			}
		})
	}
}

func TestSanitizeYAML_PreservesID(t *testing.T) {
	input := "id: arabtorrents\nname: ArabTorrents\nargs: \"(\\d+)\\/\"\n"
	sanitized := SanitizeYAML([]byte(input))

	var result struct {
		ID string `yaml:"id"`
	}
	if err := yaml.Unmarshal(sanitized, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result.ID != "arabtorrents" {
		t.Errorf("got ID=%q, want %q", result.ID, "arabtorrents")
	}
}

func TestSanitizeYAML_Passthrough(t *testing.T) {
	// YAML with no double-quoted strings should pass through unchanged.
	input := "id: test\nname: Test Indexer\nsettings:\n  - name: username\n    type: text\n"
	sanitized := SanitizeYAML([]byte(input))
	if string(sanitized) != input {
		t.Errorf("expected passthrough:\ngot:  %q\nwant: %q", string(sanitized), input)
	}
}
