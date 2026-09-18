package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func handlerCalled(t *testing.T, a *Authenticator, r *http.Request) (int, bool) {
	t.Helper()
	called := false
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code, called
}

func TestMiddlewareOhneToken(t *testing.T) {
	a := New("geheim", "")
	code, called := handlerCalled(t, a, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if code != http.StatusUnauthorized {
		t.Errorf("Status = %d, erwartet 401", code)
	}
	if called {
		t.Error("Handler wurde trotz fehlendem Token ausgeführt")
	}
}

func TestMiddlewareFalschesToken(t *testing.T) {
	a := New("geheim", "")
	r := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	r.Header.Set("Authorization", "Bearer falsch")
	code, called := handlerCalled(t, a, r)
	if code != http.StatusUnauthorized || called {
		t.Errorf("Status = %d, Handler aufgerufen = %v; erwartet 401 ohne Aufruf", code, called)
	}
}

func TestMiddlewareGueltigesToken(t *testing.T) {
	a := New("geheim", "")
	r := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	r.Header.Set("Authorization", "Bearer geheim")
	code, called := handlerCalled(t, a, r)
	if code != http.StatusOK || !called {
		t.Errorf("Status = %d, Handler aufgerufen = %v; erwartet 200 mit Aufruf", code, called)
	}
}

func TestMiddlewareTokenAlsQueryParameter(t *testing.T) {
	a := New("geheim", "")
	r := httptest.NewRequest(http.MethodGet, "/api/sessions/x/attach?token=geheim", nil)
	if code, called := handlerCalled(t, a, r); code != http.StatusOK || !called {
		t.Errorf("Status = %d, aufgerufen = %v", code, called)
	}
}

func TestMiddlewareOhneKonfiguriertesToken(t *testing.T) {
	a := New("", "")
	if code, called := handlerCalled(t, a, httptest.NewRequest(http.MethodGet, "/api/projects", nil)); code != http.StatusOK || !called {
		t.Errorf("ohne konfiguriertes Token sollte durchgereicht werden, Status = %d", code)
	}
}

func TestCheckUpgrade(t *testing.T) {
	a := New("geheim", "http://router.example:7777")

	bad := httptest.NewRequest(http.MethodGet, "/api/sessions/x/attach?token=falsch", nil)
	if err := a.CheckUpgrade(bad); err != ErrUnauthorized {
		t.Errorf("Fehler = %v, erwartet ErrUnauthorized", err)
	}

	good := httptest.NewRequest(http.MethodGet, "/api/sessions/x/attach?token=geheim", nil)
	good.Header.Set("Origin", "http://router.example:7777")
	if err := a.CheckUpgrade(good); err != nil {
		t.Errorf("unerwarteter Fehler: %v", err)
	}

	foreign := httptest.NewRequest(http.MethodGet, "/api/sessions/x/attach?token=geheim", nil)
	foreign.Header.Set("Origin", "https://boese.example")
	if err := a.CheckUpgrade(foreign); err != ErrForbiddenOrigin {
		t.Errorf("Fehler = %v, erwartet ErrForbiddenOrigin", err)
	}

	sameHost := httptest.NewRequest(http.MethodGet, "/api/sessions/x/attach?token=geheim", nil)
	sameHost.Header.Set("Origin", "http://"+sameHost.Host)
	if err := a.CheckUpgrade(sameHost); err != nil {
		t.Errorf("eigener Host sollte erlaubt sein: %v", err)
	}
}
