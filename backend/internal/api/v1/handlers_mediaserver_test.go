package apiv1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/integration/jellyfin"
	"github.com/sumia01/media-gate/internal/integration/plex"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

func TestMediaServerHTTPSettingsMappingsAndAuthorization(t *testing.T) {
	const jellyID = "00112233-4455-6677-8899-aabbccddeeff"
	var jellyScans, plexScans atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Library/VirtualFolders":
			if r.Header.Get("X-Emby-Token") != "jellyfin-test-key" {
				t.Error("saved key not decrypted")
			}
			fmt.Fprintf(w, `[{"ItemId":%q,"Name":"Movies","CollectionType":"movies","Locations":["/movies"]}]`, jellyID)
		case "/Items/" + jellyID + "/Refresh":
			jellyScans.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case "/library/sections":
			fmt.Fprint(w, `<MediaContainer><Directory key="3" title="Movies" type="movie"><Location path="/movies"/></Directory></MediaContainer>`)
		case "/library/sections/3/refresh":
			plexScans.Add(1)
		default:
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "media-server.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	lib := &store.Library{Name: "Movies", Path: "/movies", MediaType: "movie"}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(st, t.TempDir(), nil, "test-secret", upstream.Client())
	authSvc := auth.NewService(st, "test-secret")
	tokens := make(map[string]string)
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
	h := &Handlers{store: st, settings: svc,
		plexProvider:     plex.NewProvider(svc, settings.KeyPlexURL, settings.KeyPlexToken, upstream.Client()),
		jellyfinProvider: jellyfin.NewProvider(svc, settings.KeyJellyfinURL, settings.KeyJellyfinAPIKey, upstream.Client()),
	}
	router := auth.AuthMiddleware(authSvc)(HandlerWithOptions(NewStrictHandler(h, []StrictMiddlewareFunc{AdminMiddleware(authSvc)}), StdHTTPServerOptions{BaseURL: "/api/v1"}))
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
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/settings", ""}, {"PUT", "/settings", `{}`},
		{"POST", "/settings/test-jellyfin", `{}`}, {"GET", "/jellyfin/libraries", ""},
		{"GET", "/jellyfin/mappings", ""}, {"PUT", "/jellyfin/mappings", `{"mappings":[]}`},
		{"POST", "/jellyfin/refresh/" + jellyID, ""}, {"POST", "/plex/refresh/3", ""},
	} {
		request("", endpoint.method, endpoint.path, endpoint.body, 401)
		request("reader", endpoint.method, endpoint.path, endpoint.body, 403)
	}
	readSettings := func() Settings {
		t.Helper()
		w := request("admin", "GET", "/settings", "", 200)
		var result struct {
			Settings Settings `json:"settings"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Settings
	}
	initial := readSettings()
	if initial.PlexRefreshEnabled == nil || !*initial.PlexRefreshEnabled || initial.JellyfinRefreshEnabled == nil || *initial.JellyfinRefreshEnabled {
		t.Fatal("API defaults disagree with refresh policy")
	}
	request("admin", "PUT", "/settings", fmt.Sprintf(`{"plexUrl":%q,"plexToken":"plex-key","jellyfinUrl":%q,"jellyfinApiKey":"jellyfin-test-key"}`, upstream.URL, upstream.URL), 200)
	if saved := readSettings(); saved.JellyfinApiKey == nil || *saved.JellyfinApiKey != "****-key" {
		t.Fatal("API key not masked")
	}
	request("admin", "POST", "/settings/test-jellyfin", `{}`, 200)
	request("admin", "GET", "/jellyfin/libraries", "", 200)
	w := request("admin", "GET", "/jellyfin/mappings", "", 200)
	if !strings.Contains(w.Body.String(), `"autoMatched":true`) {
		t.Fatal("no mapping suggestion")
	}
	request("admin", "POST", "/jellyfin/refresh/"+jellyID, "", 409)
	request("admin", "PUT", "/settings", `{"plexRefreshEnabled":false,"jellyfinRefreshEnabled":true}`, 200)
	request("admin", "POST", "/plex/refresh/3", "", 409)
	request("admin", "POST", "/jellyfin/refresh/"+jellyID, "", 204)
	for _, server := range []string{"plex", "jellyfin"} {
		field, id := "plexSectionId", "3"
		if server == "jellyfin" {
			field, id = "jellyfinLibraryId", jellyID
		}
		for _, value := range []string{id, ""} {
			body := fmt.Sprintf(`{"mappings":[{"libraryId":%d,%q:%q}]}`, lib.ID, field, value)
			request("admin", "PUT", "/"+server+"/mappings", body, 200)
			w := request("admin", "GET", "/"+server+"/mappings", "", 200)
			if !strings.Contains(w.Body.String(), fmt.Sprintf(`%q:%q`, field, value)) || strings.Contains(w.Body.String(), `"autoMatched":true`) {
				t.Fatalf("mapping didn't persist: %s", w.Body.String())
			}
		}
	}
	request("admin", "PUT", "/settings", `{"jellyfinRefreshEnabled":false}`, 200)
	request("admin", "POST", "/jellyfin/refresh/"+jellyID, "", 409)
	if jellyScans.Load() != 1 || plexScans.Load() != 0 {
		t.Fatal("disabled server received a scan")
	}
	final := readSettings()
	if *final.PlexRefreshEnabled || *final.JellyfinRefreshEnabled {
		t.Fatal("explicit false was lost")
	}
}
