// Package auth prüft das statische Bearer-Token für die HTTP-API und für den
// WebSocket-Upgrade. Der Vergleich läuft immer konstant-zeitig.
package auth

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

// Authenticator hält das konfigurierte Token und die Basis-URL für die Origin-Prüfung.
// Ein leeres Token schaltet die Prüfung ab (nur im Modus local zulässig).
type Authenticator struct {
	token   string
	baseURL string
}

func New(token, baseURL string) *Authenticator {
	return &Authenticator{token: token, baseURL: baseURL}
}

// Enabled meldet, ob überhaupt ein Token konfiguriert ist.
func (a *Authenticator) Enabled() bool { return a != nil && a.token != "" }

// ValidToken vergleicht konstant-zeitig.
func (a *Authenticator) ValidToken(candidate string) bool {
	if !a.Enabled() {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(a.token)) == 1
}

// tokenFromRequest liest das Token aus dem Authorization-Header und — für die
// Browser-WebSocket-API, die keine Header setzen kann — aus dem Query-Parameter.
func tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if after, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
		return strings.TrimSpace(h)
	}
	return r.URL.Query().Get("token")
}

// Middleware lehnt Requests ohne gültiges Token mit 401 ab, bevor der Handler
// irgendeine Aktion ausführt. Sie wird nur vor die API gehängt; die statischen
// WebUI-Assets bleiben ohne Token erreichbar.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		if !a.ValidToken(tokenFromRequest(r)) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="project-router"`)
			http.Error(w, `{"error":"ungültiges oder fehlendes Token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CheckUpgrade prüft den WebSocket-Upgrade: Token (Query-Parameter oder Header) und
// Origin gegen die konfigurierte Basis-URL. Ein fehlender Origin-Header (Nicht-Browser-
// Clients) wird durchgelassen; ein fremder Origin nicht.
func (a *Authenticator) CheckUpgrade(r *http.Request) error {
	if a.Enabled() && !a.ValidToken(tokenFromRequest(r)) {
		return ErrUnauthorized
	}
	if err := a.CheckOrigin(r); err != nil {
		return err
	}
	return nil
}

// CheckOrigin vergleicht den Origin-Header mit der Basis-URL bzw. dem Host des
// Requests, damit eine beliebige Webseite im Browser des Nutzers nicht attachen kann.
func (a *Authenticator) CheckOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return ErrForbiddenOrigin
	}
	allowed := []string{r.Host}
	if a != nil && a.baseURL != "" {
		if b, err := url.Parse(a.baseURL); err == nil && b.Host != "" {
			allowed = append(allowed, b.Host)
		}
	}
	for _, host := range allowed {
		if strings.EqualFold(u.Host, host) {
			return nil
		}
	}
	return ErrForbiddenOrigin
}

type authError string

func (e authError) Error() string { return string(e) }

const (
	// ErrUnauthorized steht für ein fehlendes oder falsches Token (401).
	ErrUnauthorized = authError("ungültiges oder fehlendes Token")
	// ErrForbiddenOrigin steht für einen nicht erlaubten Origin-Header (403).
	ErrForbiddenOrigin = authError("Origin nicht erlaubt")
)
