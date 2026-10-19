package fileparse

import (
	"reflect"
	"testing"
)

func TestParseReleaseKeywords(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{" Ethel, ETHEL, EtHeL, Flux, FLUX, ", []string{"ethel", "flux"}},
		{" , \t, ", nil},
		{"", nil},
		{" WEB-DL , Group Name ", []string{"web-dl", "group name"}},
	} {
		if got := ParseReleaseKeywords(tc.input); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseReleaseKeywords(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
