package matching

import (
	"encoding/json"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/integration/tvdb"
)

type CreditPerson struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Type     string `json:"type"`
	Image    string `json:"image,omitempty"`
	Order    int    `json:"order"`
	Source   string `json:"source,omitempty"`
	PersonID int    `json:"personId,omitempty"`
}

func tmdbCreditsToJSON(credits *tmdb.Credits) string {
	if credits == nil {
		return ""
	}
	var people []CreditPerson

	// Top 10 cast by order
	for i, c := range credits.Cast {
		if i >= 10 {
			break
		}
		people = append(people, CreditPerson{
			Name:     c.Name,
			Role:     c.Character,
			Type:     "cast",
			Image:    c.ProfilePath,
			Order:    c.Order,
			Source:   "tmdb",
			PersonID: c.ID,
		})
	}

	// Up to 5 key crew (Director, Writer, Screenplay)
	keyJobs := map[string]bool{"Director": true, "Writer": true, "Screenplay": true}
	crewCount := 0
	for _, c := range credits.Crew {
		if crewCount >= 5 {
			break
		}
		if !keyJobs[c.Job] {
			continue
		}
		people = append(people, CreditPerson{
			Name:     c.Name,
			Role:     c.Job,
			Type:     "crew",
			Image:    c.ProfilePath,
			Order:    crewCount,
			Source:   "tmdb",
			PersonID: c.ID,
		})
		crewCount++
	}

	if len(people) == 0 {
		return ""
	}
	b, _ := json.Marshal(people)
	return string(b)
}

func tvdbCharactersToJSON(characters []tvdb.Character) string {
	if len(characters) == 0 {
		return ""
	}
	var people []CreditPerson

	castTypes := map[string]bool{"Actor": true, "Guest Star": true}
	crewTypes := map[string]bool{"Director": true, "Writer": true}
	castCount, crewCount := 0, 0

	for _, c := range characters {
		if castTypes[c.PeopleType] && castCount < 10 {
			people = append(people, CreditPerson{
				Name:     c.PersonName,
				Role:     c.Name,
				Type:     "cast",
				Image:    c.PersonImgURL,
				Order:    c.Sort,
				Source:   "tvdb",
				PersonID: c.PeopleID,
			})
			castCount++
		} else if crewTypes[c.PeopleType] && crewCount < 5 {
			people = append(people, CreditPerson{
				Name:     c.PersonName,
				Role:     c.PeopleType,
				Type:     "crew",
				Image:    c.PersonImgURL,
				Order:    crewCount,
				Source:   "tvdb",
				PersonID: c.PeopleID,
			})
			crewCount++
		}
	}

	if len(people) == 0 {
		return ""
	}
	b, _ := json.Marshal(people)
	return string(b)
}
