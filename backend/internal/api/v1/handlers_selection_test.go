package apiv1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

func TestAuthenticatedMediaSelectionLookups(t *testing.T) {
	st, err := sqlite.New(filepath.Join(t.TempDir(), "selection.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	profile := &store.MediaProfile{Name: "1080p English", Resolutions: `["1080p"]`, Languages: `["eng"]`, LanguageMode: "or"}
	if err := st.CreateMediaProfile(profile); err != nil {
		t.Fatal(err)
	}
	enabled := &store.Indexer{Name: "Enabled tracker", DefinitionID: "private-definition", Settings: `{"url":"http://private.test","password":"do-not-expose"}`, Priority: 9, SeedMinTime: 60}
	disabled := &store.Indexer{Name: "Disabled tracker", DefinitionID: "private-definition"}
	for _, idx := range []*store.Indexer{enabled, disabled} {
		if err := st.CreateIndexer(idx); err != nil {
			t.Fatal(err)
		}
	}
	disabled.Enabled = false
	if err := st.UpdateIndexer(disabled); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st, "selection-test-key")
	tokens := map[string]string{}
	for _, role := range []string{"admin", "reader"} {
		user := &store.User{Email: role + "@example.test", PasswordHash: "hash", IsAdmin: role == "admin"}
		if err := st.CreateUser(user); err != nil {
			t.Fatal(err)
		}
		token, err := authSvc.GenerateAccessToken(user)
		if err != nil {
			t.Fatal(err)
		}
		tokens[role] = token
	}
	// No indexer service: options must not load/decrypt integration credentials.
	router := auth.AuthMiddleware(authSvc)(HandlerWithOptions(NewStrictHandler(&Handlers{store: st}, []StrictMiddlewareFunc{AdminMiddleware(authSvc)}), StdHTTPServerOptions{BaseURL: "/api/v1"}))
	request := func(role, method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if tokens[role] != "" {
			r.Header.Set("Authorization", "Bearer "+tokens[role])
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s as %s: %d %s", method, path, role, w.Code, w.Body.String())
		}
		return w
	}
	for _, path := range []string{"/media-profiles", "/indexers/options"} {
		request("", "GET", path, "", http.StatusUnauthorized)
	}
	for _, role := range []string{"admin", "reader"} {
		w := request(role, "GET", "/media-profiles", "", http.StatusOK)
		var profiles ListMediaProfiles200JSONResponse
		if err := json.Unmarshal(w.Body.Bytes(), &profiles); err != nil {
			t.Fatal(err)
		}
		if len(profiles.Profiles) != 1 || profiles.Profiles[0].Id != int64(profile.ID) || profiles.Profiles[0].Name != profile.Name || len(profiles.Profiles[0].Resolutions) != 1 {
			t.Fatalf("missing selection criteria: %s", w.Body.String())
		}
		w = request(role, "GET", "/indexers/options", "", http.StatusOK)
		var result struct {
			Indexers []map[string]any `json:"indexers"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Indexers) != 1 || len(result.Indexers[0]) != 2 || result.Indexers[0]["id"] != float64(enabled.ID) || result.Indexers[0]["name"] != enabled.Name {
			t.Fatalf("unsafe or incomplete options: %s", w.Body.String())
		}
	}
	profilePath := fmt.Sprintf("/media-profiles/%d", profile.ID)
	indexerPath := fmt.Sprintf("/indexers/%d", enabled.ID)
	profileBody := `{"name":"Changed","resolutions":["720p"],"languages":["eng"]}`
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/indexers", ""}, {"GET", indexerPath, ""},
		{"POST", "/indexers", `{"name":"new","definitionId":"private-definition"}`},
		{"PUT", indexerPath, `{}`}, {"DELETE", indexerPath, ""}, {"POST", indexerPath + "/test", `{}`},
		{"POST", "/media-profiles", profileBody}, {"PUT", profilePath, profileBody}, {"DELETE", profilePath, ""},
		{"GET", "/settings", ""}, {"PUT", "/settings", `{"subtitleAutoSearch":true}`},
	} {
		request("reader", endpoint.method, endpoint.path, endpoint.body, http.StatusForbidden)
	}
	// The role fix must not prevent administrators from managing profiles.
	request("admin", "PUT", profilePath, profileBody, http.StatusOK)
	request("admin", "DELETE", profilePath, "", http.StatusNoContent)
	enabled.Enabled = false
	if err := st.UpdateIndexer(enabled); err != nil {
		t.Fatal(err)
	}
	w := request("reader", "GET", "/indexers/options", "", http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"indexers":[]}` {
		t.Fatalf("empty options should be an array: %s", w.Body.String())
	}
}
