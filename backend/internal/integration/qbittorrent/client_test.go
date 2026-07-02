package qbittorrent

import (
	"testing"
)

func TestMapState(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		expected string
	}{
		// Downloading states
		{"downloading", "downloading", "downloading"},
		{"metaDL", "metaDL", "downloading"},
		{"allocating", "allocating", "downloading"},
		{"forcedDL", "forcedDL", "downloading"},
		{"stalledDL", "stalledDL", "downloading"},
		{"queuedDL", "queuedDL", "downloading"},
		{"checkingDL", "checkingDL", "downloading"},
		{"checkingResumeData", "checkingResumeData", "downloading"},

		// Seeding states
		{"uploading", "uploading", "seeding"},
		{"forcedUP", "forcedUP", "seeding"},
		{"stalledUP", "stalledUP", "seeding"},
		{"queuedUP", "queuedUP", "seeding"},
		{"checkingUP", "checkingUP", "seeding"},

		// Completed states (qBit 4.x)
		{"pausedUP (qBit 4.x)", "pausedUP", "completed"},

		// Completed states (qBit 5.x)
		{"stoppedUP (qBit 5.x)", "stoppedUP", "completed"},

		// Paused states (qBit 4.x)
		{"pausedDL (qBit 4.x)", "pausedDL", "paused"},

		// Paused states (qBit 5.x)
		{"stoppedDL (qBit 5.x)", "stoppedDL", "paused"},

		// Moving state
		{"moving", "moving", "moving"},

		// Error states
		{"error", "error", "error"},
		{"missingFiles", "missingFiles", "error"},
		{"unknown", "unknown", "error"},

		// Default (unmapped)
		{"unmapped state", "unmappedState", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapState(tt.state)
			if got != tt.expected {
				t.Errorf("MapState(%q) = %q, want %q", tt.state, got, tt.expected)
			}
		})
	}
}
