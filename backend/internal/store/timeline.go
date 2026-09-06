package store

// TimelineEpisode is a read-only, date-windowed episode of a followed series.
type TimelineEpisode struct {
	Episode
	SeriesTitle string
	PosterPath  string
	Monitored   bool
	HasFile     bool
}
