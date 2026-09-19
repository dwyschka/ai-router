package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"project-router/internal/config"
	"project-router/internal/session"
)

// aufruf ist ein beim Testserver eingegangener Webhook-Request.
type aufruf struct {
	method string
	header http.Header
	body   string
}

func testServer(t *testing.T) (*httptest.Server, <-chan aufruf) {
	t.Helper()
	eingang := make(chan aufruf, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		eingang <- aufruf{method: r.Method, header: r.Header.Clone(), body: string(raw)}
	}))
	t.Cleanup(srv.Close)
	return srv, eingang
}

// notifyConfig baut eine vollständig normalisierte Konfiguration, wie sie der Router
// nach dem Laden hat.
func notifyConfig(t *testing.T, webhook config.WebhookConfig) config.NotifyConfig {
	t.Helper()
	cfg := config.Defaults()
	cfg.Notify.IdleAfter = config.Duration(20 * time.Millisecond)
	if webhook.URL != "" {
		cfg.Notify.Webhook.URL = webhook.URL
		if webhook.Method != "" {
			cfg.Notify.Webhook.Method = webhook.Method
		}
		if webhook.ContentType != "" {
			cfg.Notify.Webhook.ContentType = webhook.ContentType
		}
		cfg.Notify.Webhook.Headers = webhook.Headers
		cfg.Notify.Webhook.Template = webhook.Template
	}
	return cfg.Notify
}

func warteAufAufruf(t *testing.T, eingang <-chan aufruf) aufruf {
	t.Helper()
	select {
	case a := <-eingang:
		return a
	case <-time.After(3 * time.Second):
		t.Fatal("kein Webhook-Aufruf")
		return aufruf{}
	}
}

func TestWebhookSchicktJSON(t *testing.T) {
	srv, eingang := testServer(t)
	n, err := New("mein-router", "http://router.lan:7777", notifyConfig(t, config.WebhookConfig{
		URL:     srv.URL,
		Headers: map[string]string{"X-Token": "geheim"},
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	watcher := n.Watch(session.WatchMeta{
		SessionID:   "abc123",
		ProjectID:   "p1",
		ProjectName: "mein-projekt",
		RuntimeID:   "claude-code",
	}, func(string) {})
	defer watcher.Close()
	watcher.Observe([]byte("Do you want to proceed? (y/n)"))

	a := warteAufAufruf(t, eingang)
	if a.method != http.MethodPost {
		t.Errorf("Methode = %q, erwartet POST", a.method)
	}
	if got := a.header.Get("X-Token"); got != "geheim" {
		t.Errorf("X-Token = %q, erwartet den konfigurierten Header", got)
	}

	var ev Event
	if err := json.Unmarshal([]byte(a.body), &ev); err != nil {
		t.Fatalf("Body ist kein JSON: %v (%q)", err, a.body)
	}
	if ev.SessionID != "abc123" || ev.ProjectName != "mein-projekt" || ev.RuntimeID != "claude-code" {
		t.Errorf("Event = %+v, erwartet die Eckdaten der Session", ev)
	}
	if ev.Router != "mein-router" {
		t.Errorf("Router = %q, erwartet den konfigurierten Namen", ev.Router)
	}
	if !strings.Contains(ev.Message, "Do you want to proceed?") {
		t.Errorf("Message = %q, erwartet den erkannten Ausschnitt", ev.Message)
	}
	if ev.URL != "http://router.lan:7777/?session=abc123" {
		t.Errorf("URL = %q, erwartet einen Link auf die Session", ev.URL)
	}
}

func TestWebhookMitEigenemTemplate(t *testing.T) {
	srv, eingang := testServer(t)
	n, err := New("router", "", notifyConfig(t, config.WebhookConfig{
		URL:         srv.URL,
		ContentType: "text/plain",
		Template:    "{{.ProjectName}} braucht dich: {{.Message}}",
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	watcher := n.Watch(session.WatchMeta{SessionID: "s", ProjectName: "webshop"}, func(string) {})
	defer watcher.Close()
	watcher.Observe([]byte("Do you want to proceed?"))

	a := warteAufAufruf(t, eingang)
	if got, will := a.body, "webshop braucht dich: Do you want to proceed?"; got != will {
		t.Errorf("Body = %q, erwartet %q", got, will)
	}
	if ct := a.header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, erwartet text/plain", ct)
	}
}

// Ohne Template und ohne JSON-Content-Type steht eine lesbare Zeile im Body — das ist
// die Form, die ntfy und Konsorten direkt als Nachricht anzeigen.
func TestWebhookAlsTextzeile(t *testing.T) {
	srv, eingang := testServer(t)
	n, err := New("router", "http://r:7777", notifyConfig(t, config.WebhookConfig{
		URL:         srv.URL,
		ContentType: "text/plain",
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	watcher := n.Watch(session.WatchMeta{SessionID: "s1", ProjectName: "webshop", RuntimeID: "opencode"}, func(string) {})
	defer watcher.Close()
	watcher.Observe([]byte("Do you want to proceed?"))

	a := warteAufAufruf(t, eingang)
	for _, teil := range []string{"webshop", "opencode", "Do you want to proceed?", "http://r:7777/?session=s1"} {
		if !strings.Contains(a.body, teil) {
			t.Errorf("Body %q enthält %q nicht", a.body, teil)
		}
	}
}

// Ohne Webhook bleibt die Erkennung trotzdem am Werk: die Oberfläche lebt von fire.
func TestOhneWebhookMeldetNurDenClients(t *testing.T) {
	n, err := New("router", "", notifyConfig(t, config.WebhookConfig{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	gemeldet := make(chan string, 1)
	watcher := n.Watch(session.WatchMeta{SessionID: "s"}, func(e string) { gemeldet <- e })
	defer watcher.Close()
	watcher.Observe([]byte("Do you want to proceed?"))

	select {
	case msg := <-gemeldet:
		if !strings.Contains(msg, "proceed") {
			t.Errorf("Meldung = %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("keine Meldung an den Client")
	}
}

func TestKaputtesTemplateIstEinStartfehler(t *testing.T) {
	_, err := New("router", "", notifyConfig(t, config.WebhookConfig{
		URL:      "http://example.invalid",
		Template: "{{.Nicht",
	}))
	if err == nil {
		t.Fatal("erwartet ein Fehler für ein unvollständiges Template")
	}
}
