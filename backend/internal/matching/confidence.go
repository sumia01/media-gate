package matching

import (
	"strings"
	"sync"
	"unicode"
)

// levPool recycles []int buffers for the levenshtein distance computation.
// Titles are typically <100 chars, so pooled slices amortize quickly.
var levPool = sync.Pool{
	New: func() any {
		buf := make([]int, 128)
		return &buf
	},
}

func Score(itemTitle string, itemYear *int, resultTitle string, resultYear *int) float64 {
	normItem := normalize(itemTitle)
	normResult := normalize(resultTitle)

	titleSim := 1.0 - float64(levenshtein(normItem, normResult))/float64(max(len(normItem), len(normResult), 1))

	hasYear := itemYear != nil && resultYear != nil
	if !hasYear {
		return titleSim
	}

	var yearSim float64
	diff := abs(*itemYear - *resultYear)
	switch {
	case diff == 0:
		yearSim = 1.0
	case diff == 1:
		yearSim = 0.5
	default:
		yearSim = 0.0
	}

	return titleSim*0.7 + yearSim*0.3
}

func normalize(s string) string {
	s = strings.ToLower(s)
	// Remove leading articles
	for _, article := range []string{"the ", "a ", "an "} {
		s = strings.TrimPrefix(s, article)
	}
	// Remove punctuation
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// We need 2*(lb+1) ints for prev and curr rows.
	need := 2 * (lb + 1)

	bufPtr := levPool.Get().(*[]int)
	buf := *bufPtr
	if cap(buf) < need {
		buf = make([]int, need)
	} else {
		buf = buf[:need]
	}

	prev := buf[:lb+1]
	curr := buf[lb+1 : need]
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}

	result := prev[lb]
	*bufPtr = buf
	levPool.Put(bufPtr)
	return result
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
