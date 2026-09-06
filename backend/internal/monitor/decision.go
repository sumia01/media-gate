package monitor

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/store"
)

const maxDecisionDetails = 50

// Ordered by importance so a large series cannot hide a grab/error behind skips.
var decisionOutcomes = []struct{ outcome, label string }{
	{"grabbed", "Downloads queued"},
	{"error", "Failed checks"},
	{"indexer_error", "Failed searches"},
	{"no_indexers", "Searches without enabled indexers"},
	{"partial_indexer_failure", "Searches with partial indexer failures"},
	{"blocked", "Blocked selections"},
	{"profile_rejected", "Searches rejected by profile"},
	{"no_results", "Searches without results"},
	{"no_match", "Targets without a matching release"},
	{"missing_metadata", "Checks missing metadata"},
	{"active_download", "Checks covered by downloads"},
	{"already_present", "Episodes or movies already present"},
	{"unaired", "Unreleased targets"},
	{"disabled", "Unmonitored targets"},
	{"no_eligible_targets", "Checks without eligible targets"},
	{"search_results", "Searches with eligible results"},
}

type decisionCheck struct {
	row    store.MonitorDecision
	counts map[string]int
}

func newDecisionCheck(item *store.MediaItem) *decisionCheck {
	// The item may come from the cycle's earlier list; never advance this on rereads.
	inputUpdatedAt := item.UpdatedAt
	return &decisionCheck{
		row: store.MonitorDecision{
			MediaItemID: item.ID, InputUpdatedAt: &inputUpdatedAt,
			Details: make([]store.MonitorDecisionDetail, 0),
		},
		counts: make(map[string]int),
	}
}

func decisionPriority(outcome string) int {
	for i, o := range decisionOutcomes {
		if o.outcome == outcome {
			return i
		}
	}
	return len(decisionOutcomes)
}

func (c *decisionCheck) add(d store.MonitorDecisionDetail) {
	c.counts[d.Outcome]++
	if len(c.row.Details) < maxDecisionDetails {
		c.row.Details = append(c.row.Details, d)
		return
	}
	c.row.Truncated = true
	worst := -1
	priority := decisionPriority(d.Outcome)
	worstPriority := priority
	for i, existing := range c.row.Details {
		if p := decisionPriority(existing.Outcome); p > priority && p >= worstPriority {
			worst = i
			worstPriority = p
		}
	}
	if worst >= 0 {
		c.row.Details[worst] = d
	}
}

func (s *Service) saveDecision(c *decisionCheck) {
	var summary []string
	for _, o := range decisionOutcomes {
		if count := c.counts[o.outcome]; count > 0 {
			if c.row.Outcome == "" {
				c.row.Outcome = o.outcome
			}
			summary = append(summary, fmt.Sprintf("%s: %d", o.label, count))
		}
	}
	c.row.Summary = strings.Join(summary, "; ") + "."
	c.row.CheckedAt = time.Now().UTC()
	if err := s.store.UpsertMonitorDecision(&c.row); err != nil {
		slog.Error("monitor: failed to save latest decision", "item_id", c.row.MediaItemID, "error", err)
	}
}

func decisionDetail(outcome, explanation string) store.MonitorDecisionDetail {
	return store.MonitorDecisionDetail{Outcome: outcome, Explanation: explanation}
}

func (c *decisionCheck) addPartialSearchFailure(diagnostics indexer.SearchDiagnostics, season *int) {
	if diagnostics.Failed > 0 && diagnostics.Failed < diagnostics.Attempted {
		d := decisionDetail("partial_indexer_failure", fmt.Sprintf(
			"%d of %d attempted indexers failed. Results and any selection only reflect the successful indexers.",
			diagnostics.Failed, diagnostics.Attempted))
		d.SeasonNumber = season
		c.add(d)
	}
}

func searchDecision(total, eligible int, diagnostics indexer.SearchDiagnostics) store.MonitorDecisionDetail {
	d := decisionDetail("search_results", "Eligible results were returned; selection still respects episode matching and season-pack preference.")
	switch {
	case diagnostics.Attempted == 0:
		d = decisionDetail("no_indexers", "No enabled indexers are configured. Enable an indexer before auto-download can search.")
	case diagnostics.Failed == diagnostics.Attempted:
		d = decisionDetail("indexer_error", fmt.Sprintf("All %d attempted indexers failed. No reliable search results are available; the monitor will retry on its next check.", diagnostics.Attempted))
	case total == 0:
		d = decisionDetail("no_results", fmt.Sprintf("No releases were returned by %d successful indexers.", diagnostics.Attempted-diagnostics.Failed))
	case eligible == 0:
		d = decisionDetail("profile_rejected", "All returned releases were rejected by the quality profile or global exclusions.")
	}
	d.TotalResults = total
	d.RejectedResults = total - eligible
	return d
}

func safeSelectedTitle(title string) string {
	// Release titles are untrusted. Omit URL/credential-like text rather than
	// attempting to redact individual secrets from it.
	if len(title) > 240 || strings.ContainsAny(title, ":/\\?&=@%<>") || strings.IndexFunc(title, unicode.IsControl) >= 0 {
		return ""
	}
	lower := strings.ToLower(title)
	for _, marker := range []string{"apikey", "api_key", "token", "password", "passkey", "secret", "authorization"} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	return title
}
