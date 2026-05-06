package fileparse

import (
	"regexp"
	"strings"
)

// languageAliases maps various language indicators found in release titles
// to their canonical short codes. Multiple aliases may map to the same code.
var languageAliases = map[string]string{
	// Hungarian
	"hungarian": "hun", "hun": "hun", "magyar": "hun",
	// English
	"english": "eng", "eng": "eng",
	// German
	"german": "ger", "ger": "ger", "deutsch": "ger", "deu": "ger",
	// French
	"french": "fre", "fre": "fre", "fra": "fre", "français": "fre",
	// Spanish
	"spanish": "spa", "spa": "spa", "español": "spa", "esp": "spa",
	// Italian
	"italian": "ita", "ita": "ita",
	// Japanese
	"japanese": "jpn", "jpn": "jpn",
	// Korean
	"korean": "kor", "kor": "kor",
	// Russian
	"russian": "rus", "rus": "rus",
	// Portuguese
	"portuguese": "por", "por": "por",
	// Chinese
	"chinese": "chi", "chi": "chi", "zho": "chi",
	// Dutch
	"dutch": "dut", "dut": "dut", "nld": "dut",
	// Polish
	"polish": "pol", "pol": "pol",
	// Czech
	"czech": "cze", "cze": "cze", "ces": "cze",
	// Romanian
	"romanian": "rum", "rum": "rum", "ron": "rum",
	// Swedish
	"swedish": "swe", "swe": "swe",
	// Norwegian
	"norwegian": "nor", "nor": "nor",
	// Danish
	"danish": "dan", "dan": "dan",
	// Finnish
	"finnish": "fin", "fin": "fin",
	// Turkish
	"turkish": "tur", "tur": "tur",
	// Arabic
	"arabic": "ara", "ara": "ara",
	// Hindi
	"hindi": "hin", "hin": "hin",
	// Multi-language indicator
	"multi": "multi",
}

// tokenSepRe splits release titles on common separators (dot, space, underscore, hyphen).
var tokenSepRe = regexp.MustCompile(`[.\s_\-]+`)

// ParseLanguages extracts all recognized language codes from a release title.
// It tokenizes the title by common separators and matches each token against known
// language aliases. Returns a deduplicated slice of canonical language codes in
// the order they appear in the title.
//
// Examples:
//
//	"Movie.2024.1080p.HUN.ENG.BluRay" → ["hun", "eng"]
//	"Movie.2024.Hungarian.1080p" → ["hun"]
//	"Movie.2024.MULTI.1080p" → ["multi"]
//	"Movie.2024.1080p.BluRay" → []
func ParseLanguages(title string) []string {
	tokens := tokenSepRe.Split(title, -1)
	seen := make(map[string]bool)
	var langs []string

	for _, token := range tokens {
		lower := strings.ToLower(token)
		if canonical, ok := languageAliases[lower]; ok {
			if !seen[canonical] {
				seen[canonical] = true
				langs = append(langs, canonical)
			}
		}
	}

	return langs
}
