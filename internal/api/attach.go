package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"project-router/internal/auth"
	"project-router/internal/session"
)

// wsMessage ist ein Steuerframe. PTY-Bytes laufen als Binärframes, alles andere als
// JSON-Textframe — die Unterscheidung über den Frame-Typ hält den heißen Pfad frei
// von JSON.
type wsMessage struct {
	Type     string `json:"type"`
	Cols     uint16 `json:"cols,omitempty"`
	Rows     uint16 `json:"rows,omitempty"`
	Status   string `json:"status,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Message  string `json:"message,omitempty"`
}

// conn serialisiert die Schreibzugriffe auf den WebSocket.
type conn struct {
	mu sync.Mutex
	ws *websocket.Conn
}

func (c *conn) writeJSON(ctx context.Context, msg wsMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.Write(ctx, websocket.MessageText, raw)
}

func (c *conn) writeBinary(ctx context.Context, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.Write(ctx, websocket.MessageBinary, data)
}

func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	// Token und Origin werden vor dem Upgrade geprüft: ohne gültiges Token entsteht
	// keine Verbindung und es wird kein Scrollback gesendet.
	if err := s.auth.CheckUpgrade(r); err != nil {
		if errors.Is(err, auth.ErrForbiddenOrigin) {
			writeError(w, http.StatusForbidden, "Origin nicht erlaubt")
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="project-router"`)
		writeError(w, http.StatusUnauthorized, "ungültiges oder fehlendes Token")
		return
	}

	id := chi.URLParam(r, "id")
	sess, ok := s.sessions.Get(id)
	if !ok {
		if _, err := s.sessions.GetView(id); err != nil {
			writeError(w, http.StatusNotFound, "Session nicht gefunden")
			return
		}
		writeError(w, http.StatusGone, "Session ist beendet und nicht mehr im Speicher")
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Die Origin-Prüfung liegt in CheckUpgrade und damit an einer Stelle.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("WebSocket-Upgrade für Session %s fehlgeschlagen: %v", id, err)
		return
	}
	defer ws.CloseNow()
	c := &conn{ws: ws}

	size := sizeFromQuery(r)
	sub := sess.Attach(size)
	defer sub.Detach()

	ctx := r.Context()
	if sub.Truncated {
		_ = c.writeJSON(ctx, wsMessage{
			Type:    string(session.FrameTruncated),
			Message: "älterer Verlauf wurde abgeschnitten",
		})
	}
	if len(sub.Snapshot) > 0 {
		if err := c.writeBinary(ctx, sub.Snapshot); err != nil {
			return
		}
	}
	meta := sess.Meta()
	_ = c.writeJSON(ctx, wsMessage{Type: string(session.FrameStatus), Status: string(meta.Status)})

	go readPump(ctx, c, sess, sub)
	writePump(ctx, c, sub)
}

// sizeFromQuery liest die Startgröße des anfragenden Clients.
func sizeFromQuery(r *http.Request) session.Size {
	parse := func(key string) uint16 {
		v, err := strconv.ParseUint(r.URL.Query().Get(key), 10, 16)
		if err != nil {
			return 0
		}
		return uint16(v)
	}
	return session.Size{Cols: parse("cols"), Rows: parse("rows")}
}

// readPump reicht Binärframes unverändert an das PTY durch und verarbeitet
// Steuerframes des Clients.
func readPump(ctx context.Context, c *conn, sess *session.Session, sub *session.Subscription) {
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			if err := sess.Write(data); err != nil {
				if errors.Is(err, session.ErrSessionEnded) {
					_ = c.writeJSON(ctx, wsMessage{
						Type:    string(session.FrameStatus),
						Status:  string(sess.Status()),
						Message: "Session ist beendet, Eingabe wurde verworfen",
					})
					continue
				}
				return
			}
		case websocket.MessageText:
			var msg wsMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Type == "resize" {
				sub.Resize(session.Size{Cols: msg.Cols, Rows: msg.Rows})
			}
		}
	}
}

// writePump verteilt die Frames der Session an den Client.
func writePump(ctx context.Context, c *conn, sub *session.Subscription) {
	for frame := range sub.Frames {
		switch frame.Kind {
		case session.FrameData:
			if err := c.writeBinary(ctx, frame.Data); err != nil {
				return
			}
		case session.FrameExit:
			_ = c.writeJSON(ctx, wsMessage{
				Type:     string(session.FrameExit),
				Status:   string(frame.Status),
				ExitCode: frame.ExitCode,
				Message:  frame.Message,
			})
		case session.FrameStatus:
			_ = c.writeJSON(ctx, wsMessage{
				Type:    string(session.FrameStatus),
				Status:  string(frame.Status),
				Message: frame.Message,
			})
		}
	}

	// Kanal geschlossen: entweder ist die Session beendet oder dieser Client war zu
	// langsam und wurde verworfen.
	if sub.Dropped() {
		_ = c.writeJSON(ctx, wsMessage{
			Type:    string(session.FrameTruncated),
			Message: "Verbindung war zu langsam, Ausgabe wurde verworfen — bitte neu verbinden",
		})
		time.Sleep(50 * time.Millisecond) // dem Client Zeit lassen, den Hinweis zu lesen
		_ = c.ws.Close(websocket.StatusTryAgainLater, "zu langsam")
		return
	}
	_ = c.ws.Close(websocket.StatusNormalClosure, "Session beendet")
}
