package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"project-router/internal/auth"
	"project-router/internal/projects"
	"project-router/internal/session"
)

// httpServer startet den API-Router als echten HTTP-Server für WebSocket-Tests.
func (e *env) httpServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", e.Server.Routes()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// wsClient ist ein echter WebSocket-Client gegen die Attach-Route.
type wsClient struct {
	t    *testing.T
	ws   *websocket.Conn
	ctx  context.Context
	text []wsMessage
}

func dial(t *testing.T, srv *httptest.Server, sessionID, query string) (*wsClient, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/sessions/" + sessionID + "/attach"
	if query != "" {
		url += "?" + query
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	ws, resp, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, resp, err
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	return &wsClient{t: t, ws: ws, ctx: ctx}, resp, nil
}

// readUntil liest Frames, bis die gesammelten Binärdaten die Marke enthalten oder die
// Frist abläuft.
func (c *wsClient) readUntil(marke string, frist time.Duration) string {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.ctx, frist)
	defer cancel()
	var out strings.Builder
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			c.t.Fatalf("erwartet %q, bekam bis dahin %q (Fehler: %v)", marke, out.String(), err)
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
			if strings.Contains(out.String(), marke) {
				return out.String()
			}
			continue
		}
		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err == nil {
			c.text = append(c.text, msg)
		}
	}
}

func (c *wsClient) send(data []byte) {
	c.t.Helper()
	if err := c.ws.Write(c.ctx, websocket.MessageBinary, data); err != nil {
		c.t.Fatalf("senden: %v", err)
	}
}

func (c *wsClient) sendJSON(msg wsMessage) {
	c.t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.ws.Write(c.ctx, websocket.MessageText, raw); err != nil {
		c.t.Fatalf("senden: %v", err)
	}
}

// startSession legt Projekt und Session an und liefert die Session-Kennung.
func (e *env) startSession(t *testing.T, script string) string {
	t.Helper()
	e.dummyRuntime(t, "dummy", script)
	p := e.neuesProjekt("alpha")
	rec := e.do(http.MethodPost, "/api/sessions", map[string]any{
		"projectId": p.ID, "runtimeId": "dummy", "cols": 80, "rows": 24,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Session-Start: %d %s", rec.Code, rec.Body)
	}
	return decode[session.View](t, rec).ID
}

func TestAttachLueckenloserVerlaufUeberDisconnect(t *testing.T) {
	e := newEnv(t)
	// Der Agent gibt aus, was er auf stdin bekommt — damit lässt sich Ausgabe gezielt
	// vor, während und nach einer Trennung erzeugen.
	id := e.startSession(t, "while read zeile; do printf 'echo:%s\\n' \"$zeile\"; done")
	srv := e.httpServer(t)

	erst, _, err := dial(t, srv, id, "cols=80&rows=24")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	erst.send([]byte("eins\r"))
	erst.readUntil("echo:eins", 5*time.Second)

	// Trennen, während die Session weiterläuft.
	if err := erst.ws.Close(websocket.StatusNormalClosure, "tschüss"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sess, ok := e.Sessions.Get(id)
	if !ok {
		t.Fatal("Session ist nach dem Disconnect nicht mehr im Speicher")
	}
	if err := sess.Write([]byte("zwei\r")); err != nil {
		t.Fatalf("Ausgabe nach dem Disconnect: %v", err)
	}
	// Warten, bis die Ausgabe im Puffer gelandet ist.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sub := sess.Attach(session.Size{Cols: 80, Rows: 24})
		hat := strings.Contains(string(sub.Snapshot), "echo:zwei")
		sub.Detach()
		if hat {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	zweit, _, err := dial(t, srv, id, "cols=80&rows=24")
	if err != nil {
		t.Fatalf("erneutes Dial: %v", err)
	}
	verlauf := zweit.readUntil("echo:zwei", 5*time.Second)
	if !strings.Contains(verlauf, "echo:eins") {
		t.Errorf("Verlauf = %q, erwartet auch die Ausgabe vor der Trennung", verlauf)
	}
	if strings.Count(verlauf, "echo:eins") != 1 {
		t.Errorf("Verlauf enthält echo:eins mehrfach: %q", verlauf)
	}

	// Und danach geht es live weiter.
	zweit.send([]byte("drei\r"))
	live := zweit.readUntil("echo:drei", 5*time.Second)
	if live == "" {
		t.Error("kein Live-Stream nach dem Reattach")
	}
	if sess.Status() != "running" {
		t.Errorf("Status = %q, die Session sollte weiterlaufen", sess.Status())
	}
}

func TestAttachZweiGleichzeitigeClients(t *testing.T) {
	e := newEnv(t)
	id := e.startSession(t, "while read zeile; do printf 'echo:%s\\n' \"$zeile\"; done")
	srv := e.httpServer(t)

	a, _, err := dial(t, srv, id, "cols=100&rows=30")
	if err != nil {
		t.Fatalf("Dial A: %v", err)
	}
	b, _, err := dial(t, srv, id, "cols=80&rows=24")
	if err != nil {
		t.Fatalf("Dial B: %v", err)
	}

	// Eingabe von A landet am selben PTY und wird von beiden gesehen.
	a.send([]byte("von-a\r"))
	gotA := a.readUntil("echo:von-a", 5*time.Second)
	gotB := b.readUntil("echo:von-a", 5*time.Second)
	if gotA == "" || gotB == "" {
		t.Fatal("nicht beide Clients sahen die Ausgabe")
	}

	// Und umgekehrt.
	b.send([]byte("von-b\r"))
	a.readUntil("echo:von-b", 5*time.Second)
	b.readUntil("echo:von-b", 5*time.Second)

	// Das PTY folgt der kleineren Größe.
	a.sendJSON(wsMessage{Type: "resize", Cols: 100, Rows: 30})
	b.sendJSON(wsMessage{Type: "resize", Cols: 80, Rows: 24})
	sess, _ := e.Sessions.Get(id)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sess.CurrentSize() == (session.Size{Cols: 80, Rows: 24}) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Terminalgröße = %+v, erwartet das Minimum 80x24", sess.CurrentSize())
}

func TestAttachMeldetExit(t *testing.T) {
	e := newEnv(t)
	id := e.startSession(t, "printf 'fertig\\n'; sleep 0.3; exit 5")
	srv := e.httpServer(t)

	c, _, err := dial(t, srv, id, "cols=80&rows=24")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	var exit *wsMessage
	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type == "exit" {
			cp := msg
			exit = &cp
		}
	}
	if exit == nil {
		t.Fatal("kein Exit-Frame empfangen")
	}
	if exit.ExitCode == nil || *exit.ExitCode != 5 {
		t.Errorf("Exit-Code = %v, erwartet 5", exit.ExitCode)
	}
}

func TestAttachOhneGueltigesToken(t *testing.T) {
	e := newEnv(t)
	id := e.startSession(t, "sleep 5")
	e.Server.auth = auth.New("geheim", "")
	srv := e.httpServer(t)

	if _, resp, err := dial(t, srv, id, "cols=80&rows=24&token=falsch"); err == nil {
		t.Fatal("Upgrade mit falschem Token sollte scheitern")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Status = %v, erwartet 401", resp)
	}

	// Die Session läuft unverändert weiter.
	sess, ok := e.Sessions.Get(id)
	if !ok || sess.Status() != "running" {
		t.Fatal("die Session wurde durch den abgelehnten Upgrade beeinflusst")
	}

	// Mit gültigem Token klappt derselbe Aufruf.
	if _, _, err := dial(t, srv, id, "cols=80&rows=24&token=geheim"); err != nil {
		t.Fatalf("Upgrade mit gültigem Token: %v", err)
	}
}

func TestAttachUnbekannteSession(t *testing.T) {
	e := newEnv(t)
	srv := e.httpServer(t)
	_, resp, err := dial(t, srv, "gibtesnicht", "")
	if err == nil {
		t.Fatal("erwartet Fehler")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Status = %v, erwartet 404", resp)
	}
}

func TestAttachEingabeAnBeendeteSession(t *testing.T) {
	e := newEnv(t)
	id := e.startSession(t, "printf 'kurz\\n'; sleep 0.2; exit 0")
	srv := e.httpServer(t)
	sess, _ := e.Sessions.Get(id)

	c, _, err := dial(t, srv, id, "cols=80&rows=24")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	sess.Wait()
	// Nach dem Ende wird der Kanal geschlossen; der Client bekommt den Exit-Frame.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := c.ws.Read(c.ctx); err != nil {
			break
		}
	}
	if sess.Status() == "running" {
		t.Error("Session sollte beendet sein")
	}
	_ = projects.View{}
}
