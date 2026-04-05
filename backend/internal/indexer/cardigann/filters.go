package cardigann

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ApplyFilters runs a pipeline of named filters on a value.
func ApplyFilters(value string, filters []Filter) (string, error) {
	var err error
	for _, f := range filters {
		value, err = applyFilter(value, f)
		if err != nil {
			return "", fmt.Errorf("filter %q: %w", f.Name, err)
		}
	}
	return value, nil
}

func applyFilter(value string, f Filter) (string, error) {
	switch f.Name {
	case "querystring":
		return filterQuerystring(value, f.Args)
	case "replace":
		return filterReplace(value, f.Args)
	case "regexp":
		return filterRegexp(value, f.Args)
	case "re_replace":
		return filterReReplace(value, f.Args)
	case "dateparse":
		return filterDateparse(value, f.Args)
	case "append":
		return filterAppend(value, f.Args)
	case "prepend":
		return filterPrepend(value, f.Args)
	case "split":
		return filterSplit(value, f.Args)
	case "urldecode":
		return filterUrldecode(value)
	case "fuzzytime":
		return filterFuzzytime(value)
	default:
		return value, nil
	}
}

func filterQuerystring(value string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("querystring requires a key argument")
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	return u.Query().Get(args[0]), nil
}

func filterReplace(value string, args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("replace requires old and new arguments")
	}
	return strings.ReplaceAll(value, args[0], args[1]), nil
}

func filterRegexp(value string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("regexp requires a pattern argument")
	}
	re, err := regexp.Compile(args[0])
	if err != nil {
		return "", err
	}
	matches := re.FindStringSubmatch(value)
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) > 1 {
		return matches[1], nil
	}
	return matches[0], nil
}

func filterReReplace(value string, args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("re_replace requires pattern and replacement arguments")
	}
	re, err := regexp.Compile(args[0])
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(value, args[1]), nil
}

// filterDateparse converts a date string using the Cardigann date layout.
// Cardigann uses Java/C#-style format tokens (yyyy, MM, dd, HH, mm, ss, zzz).
func filterDateparse(value string, args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("dateparse requires a layout argument")
	}
	layout := cardigannToGoLayout(args[0])
	t, err := time.Parse(layout, value)
	if err != nil {
		return "", fmt.Errorf("parsing date %q with layout %q (Go: %q): %w", value, args[0], layout, err)
	}
	return strconv.FormatInt(t.Unix(), 10), nil
}

func filterAppend(value string, args []string) (string, error) {
	if len(args) < 1 {
		return value, nil
	}
	return value + args[0], nil
}

func filterPrepend(value string, args []string) (string, error) {
	if len(args) < 1 {
		return value, nil
	}
	return args[0] + value, nil
}

func filterSplit(value string, args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("split requires separator and index arguments")
	}
	sep := args[0]
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return "", fmt.Errorf("split index %q: %w", args[1], err)
	}
	parts := strings.Split(value, sep)
	if idx < 0 || idx >= len(parts) {
		return "", nil
	}
	return parts[idx], nil
}

func filterUrldecode(value string) (string, error) {
	return url.QueryUnescape(value)
}

func filterFuzzytime(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	now := time.Now()

	if value == "now" || value == "just now" {
		return strconv.FormatInt(now.Unix(), 10), nil
	}
	if value == "yesterday" {
		return strconv.FormatInt(now.AddDate(0, 0, -1).Unix(), 10), nil
	}
	if value == "today" {
		return strconv.FormatInt(now.Unix(), 10), nil
	}

	re := regexp.MustCompile(`(\d+)\s*(second|minute|hour|day|week|month|year)s?\s*ago`)
	m := re.FindStringSubmatch(value)
	if len(m) == 3 {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "second":
			return strconv.FormatInt(now.Add(-time.Duration(n)*time.Second).Unix(), 10), nil
		case "minute":
			return strconv.FormatInt(now.Add(-time.Duration(n)*time.Minute).Unix(), 10), nil
		case "hour":
			return strconv.FormatInt(now.Add(-time.Duration(n)*time.Hour).Unix(), 10), nil
		case "day":
			return strconv.FormatInt(now.AddDate(0, 0, -n).Unix(), 10), nil
		case "week":
			return strconv.FormatInt(now.AddDate(0, 0, -7*n).Unix(), 10), nil
		case "month":
			return strconv.FormatInt(now.AddDate(0, -n, 0).Unix(), 10), nil
		case "year":
			return strconv.FormatInt(now.AddDate(-n, 0, 0).Unix(), 10), nil
		}
	}

	return value, nil
}

// cardigannToGoLayout converts Cardigann date format tokens to Go time layout.
func cardigannToGoLayout(layout string) string {
	r := strings.NewReplacer(
		"yyyy", "2006",
		"yy", "06",
		"MMMM", "January",
		"MMM", "Jan",
		"MM", "01",
		"dd", "02",
		"HH", "15",
		"hh", "03",
		"mm", "04",
		"ss", "05",
		"zzz", "-07:00",
		"zz", "-07",
		"tt", "PM",
	)
	return r.Replace(layout)
}
