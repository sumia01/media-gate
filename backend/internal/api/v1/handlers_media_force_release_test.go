package apiv1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdateMediaItemForceReleaseRoundTrip(t *testing.T) {
	h, st, item, ctx := monitorToggleFixture(t)
	if !item.ForceRelease {
		t.Fatal("new item should default to force mode")
	}
	for _, force := range []bool{false, false, true} {
		response, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
			Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{ForceRelease: &force},
		})
		if err != nil {
			t.Fatal(err)
		}
		apiItem, ok := response.(UpdateMediaItem200JSONResponse)
		if !ok || apiItem.ForceRelease == nil || *apiItem.ForceRelease != force {
			t.Fatalf("response lost mode %v: %+v", force, response)
		}
		encoded, err := json.Marshal(apiItem)
		if err != nil || !strings.Contains(string(encoded), `"forceRelease":`) {
			t.Fatalf("mode omitted from JSON: %s, %v", encoded, err)
		}
		fresh, err := st.GetMediaItem(item.ID)
		if err != nil || fresh.ForceRelease != force {
			t.Fatalf("saved mode = %+v, %v", fresh, err)
		}
		// Updating only the list must not reset the mode, especially false.
		keywords := "ETHEL, FLUX"
		if _, err := h.UpdateMediaItem(ctx, UpdateMediaItemRequestObject{
			Id: int64(item.ID), Body: &UpdateMediaItemJSONRequestBody{PreferredRelease: &keywords},
		}); err != nil {
			t.Fatal(err)
		}
		readResponse, err := h.GetMediaItem(ctx, GetMediaItemRequestObject{Id: int64(item.ID)})
		if err != nil {
			t.Fatal(err)
		}
		got := readResponse.(GetMediaItem200JSONResponse)
		if got.ForceRelease == nil || *got.ForceRelease != force || got.PreferredRelease == nil || *got.PreferredRelease != "ethel, flux" {
			t.Fatalf("omitted mode changed saved settings: %+v", got)
		}
	}
	rows := mediaActivityRows(t, st, item.ID, ctx)
	if len(rows) != 3 {
		t.Fatalf("expected two mode changes and one keyword change, got %+v", rows)
	}
	for _, change := range []struct {
		row           int
		before, after string
	}{{0, "false", "true"}, {2, "true", "false"}} {
		details := mediaActivityDetails(t, rows[change.row])
		if len(details.FieldChanges) != 1 || details.FieldChanges[0].Field != "force_release" ||
			details.FieldChanges[0].Before == nil || *details.FieldChanges[0].Before != change.before ||
			details.FieldChanges[0].After == nil || *details.FieldChanges[0].After != change.after {
			t.Fatalf("mode activity = %+v", details)
		}
	}
}
