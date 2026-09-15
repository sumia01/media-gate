package tmdb

import "testing"

func TestGetPersonIncludesCombinedCredits(t *testing.T) {
	var requests []string
	queries := map[string]map[string]string{}
	client := suggestionsTestClient(t, map[string]string{
		"/person/287": `{
			"id":287,
			"name":"Brad Pitt",
			"biography":"Biography",
			"profile_path":"/profile.jpg",
			"known_for_department":"Acting",
			"combined_credits":{"cast":[
				{"id":550,"media_type":"movie","title":"Fight Club","release_date":"1999-10-15","popularity":50},
				{"id":1399,"media_type":"tv","name":"Game of Thrones","first_air_date":"2011-04-17","popularity":40}
			]}
		}`,
	}, &requests, &queries)

	person, err := client.GetPerson(287)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0] != "/person/287" {
		t.Fatalf("requests = %v", requests)
	}
	if queries["/person/287"]["append_to_response"] != "combined_credits" {
		t.Errorf("append_to_response = %q", queries["/person/287"]["append_to_response"])
	}
	if person.Name != "Brad Pitt" || len(person.CombinedCredits.Cast) != 2 {
		t.Fatalf("unexpected person: %+v", person)
	}
	if person.CombinedCredits.Cast[0].DisplayTitle() != "Fight Club" || person.CombinedCredits.Cast[1].Date() != "2011-04-17" {
		t.Errorf("unexpected credit helpers: %+v", person.CombinedCredits.Cast)
	}
}

func TestResolvePersonRequiresUnambiguousExactMatch(t *testing.T) {
	tests := []struct {
		name        string
		profilePath string
		response    string
		want        int
	}{
		{
			name:        "Alex Smith",
			profilePath: "/right.jpg",
			response:    `{"results":[{"id":1,"name":"Alex Smith","profile_path":"/other.jpg"},{"id":2,"name":"Alex Smith","profile_path":"/right.jpg"}]}`,
			want:        2,
		},
		{
			name:     "Unique Actor",
			response: `{"results":[{"id":3,"name":"unique actor","profile_path":"/actor.jpg"},{"id":4,"name":"Different Person"}]}`,
			want:     3,
		},
		{
			name:     "Alex Smith",
			response: `{"results":[{"id":1,"name":"Alex Smith"},{"id":2,"name":"Alex Smith"}]}`,
			want:     0,
		},
		{
			name:     "Unique On First Page",
			response: `{"total_pages":2,"results":[{"id":5,"name":"Unique On First Page"}]}`,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+tt.profilePath, func(t *testing.T) {
			var requests []string
			queries := map[string]map[string]string{}
			client := suggestionsTestClient(t, map[string]string{"/search/person": tt.response}, &requests, &queries)
			got, err := client.ResolvePerson(tt.name, tt.profilePath)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("ResolvePerson() = %d, want %d", got, tt.want)
			}
			if queries["/search/person"]["query"] != tt.name {
				t.Errorf("query = %q", queries["/search/person"]["query"])
			}
		})
	}
}
