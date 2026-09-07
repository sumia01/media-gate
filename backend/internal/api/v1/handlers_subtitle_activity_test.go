package apiv1

import (
	"context"
	"errors"
	"testing"
)

func TestManualSubtitleMutationsRequireActor(t *testing.T) {
	h := &Handlers{}
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "download",
			call: func() error {
				_, err := h.DownloadSubtitle(context.Background(), DownloadSubtitleRequestObject{Body: &SubtitleDownloadRequest{}})
				return err
			},
		},
		{
			name: "delete",
			call: func() error {
				_, err := h.DeleteSubtitle(context.Background(), DeleteSubtitleRequestObject{})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, errMediaActivityActorMissing) {
				t.Fatalf("error = %v, want missing actor", err)
			}
		})
	}
}
