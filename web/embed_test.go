package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPAFallback(t *testing.T) {
	h := Handler()

	tests := []struct {
		name string
		path string
	}{
		{"Wurzel", "/"},
		{"unbekannter Pfad", "/sessions/abc123"},
		{"tiefer Pfad", "/projekte/alpha/terminal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("Status = %d, erwartet 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "<html") && !strings.Contains(rec.Body.String(), "<!doctype") {
				t.Errorf("Antwort ist kein index.html: %q", rec.Body.String())
			}
		})
	}
}

func TestVorhandeneDateiWirdDirektGeliefert(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d", rec.Code)
	}
}
