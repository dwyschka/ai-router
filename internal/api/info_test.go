package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"project-router/internal/auth"
	"project-router/internal/config"
)

// infoServer baut einen Server, der nur für /info reicht.
func infoServer(name, token string) *Server {
	cfg := config.Defaults()
	cfg.Name = name
	cfg.Token = token
	return NewServer(Options{Config: cfg, Auth: auth.New(token, "")})
}

func holeInfo(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func TestInfoLiefertDenKonfiguriertenNamen(t *testing.T) {
	rec := holeInfo(t, infoServer("Homelab-Router", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, erwartet 200", rec.Code)
	}
	got := decode[info](t, rec)
	if got.Name != "Homelab-Router" {
		t.Errorf("Name = %q, erwartet den konfigurierten Namen", got.Name)
	}
	if got.RequiresToken {
		t.Error("RequiresToken = true, erwartet false ohne Token")
	}
}

// /info liegt bewusst vor der Auth-Middleware: die Oberfläche braucht den Namen
// schon, bevor der Nutzer ein Token eingegeben hat.
func TestInfoBrauchtKeinToken(t *testing.T) {
	rec := holeInfo(t, infoServer("geschützt", "geheim"))
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, erwartet 200 auch ohne Token", rec.Code)
	}
	got := decode[info](t, rec)
	if got.Name != "geschützt" {
		t.Errorf("Name = %q", got.Name)
	}
	if !got.RequiresToken {
		t.Error("RequiresToken = false, erwartet true bei gesetztem Token")
	}
}

// Die übrigen Endpunkte bleiben hinter der Prüfung.
func TestUebrigeEndpunkteBleibenGeschuetzt(t *testing.T) {
	srv := infoServer("geschützt", "geheim")
	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Status = %d, erwartet 401 ohne Token", rec.Code)
	}
}

func TestInfoOhneNamenFaelltAufDenDefaultZurueck(t *testing.T) {
	srv := infoServer("", "")
	got := decode[info](t, holeInfo(t, srv))
	if got.Name != config.DefaultName {
		t.Errorf("Name = %q, erwartet %q", got.Name, config.DefaultName)
	}
}
