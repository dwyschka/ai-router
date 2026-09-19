package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"text/template"
	"time"

	"project-router/internal/config"
	"project-router/internal/session"
)

// Event ist eine erkannte Rückfrage. Die Felder sind zugleich die Daten, die einem
// eigenen Body-Template zur Verfügung stehen.
type Event struct {
	// Router ist der konfigurierte Anzeigename dieser Instanz.
	Router      string `json:"router"`
	Event       string `json:"event"`
	SessionID   string `json:"session"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"project"`
	RuntimeID   string `json:"runtime"`
	// Message ist der erkannte Ausschnitt aus der Ausgabe.
	Message string `json:"message"`
	// URL zeigt auf die Session in der WebUI, sofern eine baseURL konfiguriert ist.
	URL  string    `json:"url,omitempty"`
	Time time.Time `json:"time"`
}

// Text ist die einzeilige Fassung für Ziele, die keinen JSON-Body wollen.
func (e Event) Text() string {
	name := e.ProjectName
	if name == "" {
		name = e.ProjectID
	}
	line := fmt.Sprintf("%s: %s (%s) wartet auf eine Entscheidung — %s", e.Router, name, e.RuntimeID, e.Message)
	if e.URL != "" {
		line += "\n" + e.URL
	}
	return line
}

// Notifier erzeugt die Beobachter der Sessions und schickt erkannte Rückfragen an
// den konfigurierten Webhook.
type Notifier struct {
	name     string
	baseURL  string
	webhook  config.WebhookConfig
	patterns []*regexp.Regexp
	idle     time.Duration
	tmpl     *template.Template
	client   *http.Client
}

// New baut den Notifier. Ein fehlerhaftes Muster oder Template ist ein Startfehler:
// besser jetzt als bei der ersten Rückfrage.
func New(name, baseURL string, cfg config.NotifyConfig) (*Notifier, error) {
	patterns, err := CompilePatterns(cfg.Patterns, cfg.ReplacePatterns)
	if err != nil {
		return nil, fmt.Errorf("notify: Muster konnte nicht übersetzt werden: %w", err)
	}

	n := &Notifier{
		name:     name,
		baseURL:  strings.TrimRight(baseURL, "/"),
		webhook:  cfg.Webhook,
		patterns: patterns,
		idle:     cfg.IdleAfter.Duration(),
		client:   &http.Client{Timeout: cfg.Webhook.Timeout.Duration()},
	}
	if vorlage := strings.TrimSpace(cfg.Webhook.Template); vorlage != "" {
		tmpl, err := template.New("webhook").Parse(vorlage)
		if err != nil {
			return nil, fmt.Errorf("notify.webhook.template ist kein gültiges Template: %w", err)
		}
		n.tmpl = tmpl
	}
	return n, nil
}

// Watch ist die session.WatchFunc dieses Notifiers: pro Session ein Detector, dessen
// Treffer an die angehängten Clients und an den Webhook gehen.
func (n *Notifier) Watch(meta session.WatchMeta, fire func(excerpt string)) session.Watcher {
	return NewDetector(n.patterns, n.idle, func(excerpt string) {
		// Erst die Clients: der Browser, der gerade offen ist, soll nicht auf den
		// Umweg über den Webhook warten.
		fire(excerpt)
		n.Send(Event{
			Router:      n.name,
			Event:       "attention",
			SessionID:   meta.SessionID,
			ProjectID:   meta.ProjectID,
			ProjectName: meta.ProjectName,
			RuntimeID:   meta.RuntimeID,
			Message:     excerpt,
			URL:         n.sessionURL(meta.SessionID),
			Time:        time.Now().UTC(),
		})
	})
}

// sessionURL zeigt direkt auf die betroffene Session, sofern eine baseURL bekannt ist.
func (n *Notifier) sessionURL(id string) string {
	if n.baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/?session=%s", n.baseURL, id)
}

// Send stellt das Ereignis zu. Der Aufruf kehrt sofort zurück: ein hängender Webhook
// darf die Session nicht bremsen, und ein Fehler landet im Log statt im Terminal.
func (n *Notifier) Send(ev Event) {
	if !n.webhook.Configured() {
		return
	}
	go func() {
		if err := n.send(ev); err != nil {
			log.Printf("Benachrichtigung für Session %s fehlgeschlagen: %v", ev.SessionID, err)
		}
	}()
}

func (n *Notifier) send(ev Event) error {
	body, err := n.body(ev)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), n.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, n.webhook.Method, n.webhook.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", n.webhook.ContentType)
	for k, v := range n.webhook.Headers {
		req.Header.Set(k, v)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s antwortete mit %s", n.webhook.Method, n.webhook.URL, resp.Status)
	}
	return nil
}

// body baut den Request-Body: das eigene Template, sonst JSON oder eine Textzeile —
// je nach Content-Type.
func (n *Notifier) body(ev Event) ([]byte, error) {
	if n.tmpl != nil {
		var buf bytes.Buffer
		if err := n.tmpl.Execute(&buf, ev); err != nil {
			return nil, fmt.Errorf("notify.webhook.template: %w", err)
		}
		return buf.Bytes(), nil
	}
	if strings.Contains(n.webhook.ContentType, "json") {
		return json.Marshal(ev)
	}
	return []byte(ev.Text()), nil
}
