package matching

import (
	"encoding/json"
	"testing"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
)

func TestCreditJSONRetainsProviderPersonIdentity(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		wantSource string
		wantID     int
	}{
		{
			name: "tmdb",
			json: tmdbCreditsToJSON(&tmdb.Credits{Cast: []tmdb.CastMember{{
				ID: 287, Name: "Brad Pitt", Character: "Tyler Durden", ProfilePath: "/profile.jpg",
			}}}),
			wantSource: "tmdb",
			wantID:     287,
		},
		{
			name: "tvdb",
			json: tvdbCharactersToJSON([]tvdb.Character{{
				PeopleID: 1234, PersonName: "Actor", PeopleType: "Actor", Name: "Character",
			}}),
			wantSource: "tvdb",
			wantID:     1234,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var people []CreditPerson
			if err := json.Unmarshal([]byte(tt.json), &people); err != nil {
				t.Fatal(err)
			}
			if len(people) != 1 || people[0].Source != tt.wantSource || people[0].PersonID != tt.wantID {
				t.Fatalf("unexpected credits: %+v", people)
			}
		})
	}
}
