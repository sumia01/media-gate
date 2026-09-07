package apiv1

import (
	"context"
	"errors"
	"testing"
)

func TestManualDownloadMutationsRequireActor(t *testing.T) {
	h := &Handlers{}
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "create",
			call: func() error {
				_, err := h.CreateDownload(context.Background(), CreateDownloadRequestObject{Body: &DownloadCreate{}})
				return err
			},
		},
		{
			name: "status",
			call: func() error {
				_, err := h.UpdateDownloadStatus(context.Background(), UpdateDownloadStatusRequestObject{
					Body: &DownloadStatusUpdate{Status: DownloadStatusUpdateStatus("pending")},
				})
				return err
			},
		},
		{
			name: "delete",
			call: func() error {
				_, err := h.DeleteDownload(context.Background(), DeleteDownloadRequestObject{})
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, errMediaActivityActorMissing) {
				t.Fatalf("error = %v, want missing actor", err)
			}
		})
	}
}
