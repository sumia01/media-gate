package apiv1

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
)

func TestSavedReleaseKeywordsNormalizeAndRemainPrivate(t *testing.T) {
	for _, force := range []bool{false, true} {
		h, st, item, ctx := monitorToggleFixture(t)
		value := " Ethel, ETHEL, EtHeL, Flux, FLUX , "
		response, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
			Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &value, ForceRelease: &force},
		})
		if err != nil {
			t.Fatal(err)
		}
		got := response.(UpdateMediaItem200JSONResponse)
		if got.PreferredRelease == nil || *got.PreferredRelease != "ethel, flux" {
			t.Fatalf("keywords not canonical: %+v", got)
		}
		userID, _ := auth.UserIDFromContext(ctx)
		other := &store.User{Email: "other@example.test", PasswordHash: "hash"}
		if err := st.CreateUser(other); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			user   uint
			prefix string
			want   []string
		}{
			{userID, "eTh", []string{"ethel"}},
			{userID, "ehtel", []string{"ethel"}},
			{userID, "thel", []string{"ethel"}},
			{userID, "flx", []string{"flux"}},
			{userID, "FL", []string{"flux"}},
			{other.ID, "ET", []string{}},
		} {
			response, err := h.ListReleaseKeywords(auth.ContextWithUserID(context.Background(), tc.user), ListReleaseKeywordsRequestObject{
				Params: ListReleaseKeywordsParams{Prefix: tc.prefix},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := response.(ListReleaseKeywords200JSONResponse).Keywords; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("private suggestions = %v, want %v", got, tc.want)
			}
		}
		empty := " , "
		if _, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
			Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &empty},
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.DeleteMediaItem(item.ID); err != nil {
			t.Fatal(err)
		}
		if got, err := st.ListReleaseKeywords(userID, "e"); err != nil || !reflect.DeepEqual(got, []string{"ethel"}) {
			t.Fatalf("clearing/deleting media erased prior usage: %v, %v", got, err)
		}
	}
}

func TestReleaseKeywordHistoryRollsBackWithSettingsActivity(t *testing.T) {
	h, st, item, ctx := monitorToggleFixture(t)
	h.store = &monitorTransactionStore{Store: st, fail: "activity"}
	value := "ETHEL"
	if _, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
		Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &value},
	}); !errors.Is(err, monitorWriteError) {
		t.Fatalf("error = %v", err)
	}
	userID, _ := auth.UserIDFromContext(ctx)
	if got, err := st.ListReleaseKeywords(userID, "e"); err != nil || len(got) != 0 {
		t.Fatalf("failed save remembered keywords: %v, %v", got, err)
	}
	fresh, err := st.GetMediaItem(item.ID)
	if err != nil || fresh.PreferredRelease != "" {
		t.Fatalf("failed save changed settings: %+v, %v", fresh, err)
	}
}

func TestReleaseKeywordSuggestionsRequireAuthAndValidPrefix(t *testing.T) {
	h, _, _, ctx := monitorToggleFixture(t)
	response, err := h.ListReleaseKeywords(context.Background(), ListReleaseKeywordsRequestObject{Params: ListReleaseKeywordsParams{Prefix: "e"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.(ListReleaseKeywords401JSONResponse); !ok {
		t.Fatalf("missing authentication accepted: %+v", response)
	}
	for _, prefix := range []string{"", " \t ", strings.Repeat("x", 257)} {
		response, err := h.ListReleaseKeywords(ctx, ListReleaseKeywordsRequestObject{Params: ListReleaseKeywordsParams{Prefix: prefix}})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := response.(ListReleaseKeywords400JSONResponse); !ok {
			t.Fatalf("invalid prefix %q accepted: %+v", prefix, response)
		}
	}
}
