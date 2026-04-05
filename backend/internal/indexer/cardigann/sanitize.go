package cardigann

// SanitizeYAML fixes invalid escape sequences in double-quoted YAML strings.
//
// Prowlarr's C# YAML parser (YamlDotNet) treats unknown escape sequences like
// \d or \/ as literal characters. Go's yaml.v3 is strict (YAML 1.2) and rejects
// them. This function doubles the backslash for any escape that yaml.v3 would
// reject, so \d becomes \\d (literal backslash + d).
func SanitizeYAML(data []byte) []byte {
	// Valid escape characters after \ in yaml.v3 double-quoted scalars.
	// Source: gopkg.in/yaml.v3 scannerc.go switch cases.
	valid := [256]bool{}
	for _, c := range []byte("0abt\tnvfre \"'\\\x00NLPxuU_ ") {
		valid[c] = true
	}

	out := make([]byte, 0, len(data))
	i := 0
	n := len(data)

	for i < n {
		b := data[i]

		// Skip single-quoted strings entirely (no escape processing in YAML).
		if b == '\'' {
			out = append(out, b)
			i++
			for i < n {
				if data[i] == '\'' {
					if i+1 < n && data[i+1] == '\'' {
						// Escaped single quote inside single-quoted string.
						out = append(out, '\'', '\'')
						i += 2
						continue
					}
					out = append(out, '\'')
					i++
					break
				}
				out = append(out, data[i])
				i++
			}
			continue
		}

		// Enter double-quoted string.
		if b == '"' {
			out = append(out, b)
			i++
			for i < n {
				c := data[i]
				if c == '"' {
					// End of double-quoted string.
					out = append(out, c)
					i++
					break
				}
				if c == '\\' && i+1 < n {
					next := data[i+1]
					if next == '\n' || next == '\r' {
						// Escaped line break — valid, pass through.
						out = append(out, c, next)
						i += 2
						continue
					}
					if !valid[next] {
						// Unknown escape: double the backslash.
						out = append(out, '\\', '\\', next)
						i += 2
						continue
					}
					// Valid escape — pass through.
					out = append(out, c, next)
					i += 2
					continue
				}
				out = append(out, c)
				i++
			}
			continue
		}

		// Skip comments (# to end of line) — no processing needed.
		if b == '#' {
			for i < n && data[i] != '\n' {
				out = append(out, data[i])
				i++
			}
			continue
		}

		out = append(out, b)
		i++
	}

	return out
}
