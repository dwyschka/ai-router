package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"project-router/internal/auth"
	"project-router/internal/fsbrowse"
)

// fsServer baut ein temporäres Verzeichnisgerüst samt Server ohne Token.
func fsServer(t *testing.T) (*Server, string) {
	t.Helper()
	e := newEnv(t)
	e.repo("beta")
	if err := os.MkdirAll(filepath.Join(e.Root, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.Root, "notiz.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e.Server, e.Base
}

func getFS(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/fs"
	if path != "" {
		target += "?path=" + url.QueryEscape(path)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	http.StripPrefix("/api", s.Routes()).ServeHTTP(rec, req)
	return rec
}

func TestHandleFSRoots(t *testing.T) {
	s, base := fsServer(t)
	rec := getFS(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	var listing fsbrowse.Listing
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if !listing.IsRoots || len(listing.Entries) != 1 || listing.Entries[0].Path != filepath.Join(base, "projects") {
		t.Fatalf("unerwartete Roots-Antwort: %+v", listing)
	}
}

func TestHandleFSUnterverzeichnis(t *testing.T) {
	s, base := fsServer(t)
	rec := getFS(t, s, filepath.Join(base, "projects"))
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d: %s", rec.Code, rec.Body)
	}
	var listing fsbrowse.Listing
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("Einträge = %+v, erwartet alpha und beta ohne Datei", listing.Entries)
	}
	if listing.Entries[0].Name != "alpha" || !listing.Entries[1].IsGitRepo {
		t.Errorf("Sortierung oder Git-Markierung falsch: %+v", listing.Entries)
	}
}

func TestHandleFSStatuscodes(t *testing.T) {
	s, base := fsServer(t)
	root := filepath.Join(base, "projects")

	tests := []struct {
		name string
		path string
		want int
	}{
		{"außerhalb der Roots", "/etc", http.StatusForbidden},
		{"Traversal", filepath.Join(root, "..", ".."), http.StatusForbidden},
		{"nicht existent außerhalb der Roots", filepath.Join(base, "gibtesnicht"), http.StatusForbidden},
		{"nicht existent innerhalb der Roots", filepath.Join(root, "gibtesnicht"), http.StatusNotFound},
		{"reguläre Datei", filepath.Join(root, "notiz.txt"), http.StatusBadRequest},
		{"gültig", root, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := getFS(t, s, tc.path)
			if rec.Code != tc.want {
				t.Fatalf("Status = %d, erwartet %d (Body: %s)", rec.Code, tc.want, rec.Body)
			}
			if tc.want == http.StatusForbidden && rec.Body.Len() > 0 {
				var body errorBody
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error == "" {
					t.Error("403 sollte eine Fehlermeldung tragen")
				}
			}
		})
	}
}

func TestHandleFSKeineLeseberechtigung(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("als root greifen Dateirechte nicht")
	}
	s, base := fsServer(t)
	gesperrt := filepath.Join(base, "projects", "gesperrt")
	if err := os.MkdirAll(gesperrt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gesperrt, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gesperrt, 0o755) })

	rec := getFS(t, s, gesperrt)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Status = %d, erwartet 403", rec.Code)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" {
		t.Error("Fehlermeldung sollte die fehlende Berechtigung benennen")
	}
}

func TestHandleFSOhneToken(t *testing.T) {
	s, base := fsServer(t)
	s.auth = auth.New("geheim", "")
	rec := getFS(t, s, filepath.Join(base, "projects"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Status = %d, erwartet 401", rec.Code)
	}
}
