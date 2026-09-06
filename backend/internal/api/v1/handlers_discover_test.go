package apiv1

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/sumia01/media-gate/internal/integration/tmdb"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

type discoverHandlerStore struct {
	store.Store
	metas []store.MediaMetadata
	err   error
}

func (s *discoverHandlerStore) ListMediaMetadataExternalIDs() ([]store.MediaMetadata, error) {
	return s.metas, s.err
}

func (s *discoverHandlerStore) GetSetting(string) (*store.Setting, error) {
	return nil, store.ErrNotFound
}

func TestGetMediaExternalIdsMediaType(t *testing.T) {
	s := &discoverHandlerStore{metas: []store.MediaMetadata{
		{MediaItemID: 1, Source: "tmdb", ExternalID: 42, MediaType: "movie"},
		{MediaItemID: 2, Source: "tmdb", ExternalID: 42, MediaType: "series"},
		{MediaItemID: 3, Source: "tvdb", ExternalID: 42, MediaType: "series"},
	}}
	h := &Handlers{store: s}
	response, err := h.GetMediaExternalIds(context.Background(), GetMediaExternalIdsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got := response.(GetMediaExternalIds200JSONResponse)
	if len(got.Items) != len(s.metas) {
		t.Fatalf("got %d identities", len(got.Items))
	}
	for i, item := range got.Items {
		want := s.metas[i]
		if !item.MediaType.Valid() || string(item.MediaType) != want.MediaType || item.ExternalId != want.ExternalID || item.Source != want.Source || item.MediaItemId != int(want.MediaItemID) {
			t.Errorf("got %#v, want identity from %#v", item, want)
		}
	}
	s.err = errors.New("store unavailable")
	if _, err := h.GetMediaExternalIds(context.Background(), GetMediaExternalIdsRequestObject{}); !errors.Is(err, s.err) {
		t.Fatalf("membership error must propagate: %v", err)
	}
}

func TestFetchDiscoverDistinguishesProviderFailureFromEmpty(t *testing.T) {
	s := &discoverHandlerStore{}
	for _, key := range []string{"", "test-key"} {
		t.Run(key, func(t *testing.T) {
			client := &http.Client{}
			set := settings.NewService(s, t.TempDir(), map[string]string{settings.KeyTMDBApiKey: key}, "", client)
			h := &Handlers{matchSvc: matching.NewService(s, set, t.TempDir(), client)}
			providerErr := errors.New("provider unavailable")
			called := false
			items, pages, err := h.fetchDiscover(func(*tmdb.Client) ([]DiscoverItem, int, error) {
				called = true
				return nil, 0, providerErr
			})
			if key == "" {
				if called || err != nil || len(items) != 0 || pages != 0 {
					t.Fatalf("missing key should be empty without network calls: %v, %v, %v", items, pages, err)
				}
			} else if !called || !errors.Is(err, providerErr) {
				t.Fatalf("provider error must reach the frontend for retry: %v", err)
			}
		})
	}
}
