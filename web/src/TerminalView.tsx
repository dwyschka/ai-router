import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { api, attachURL, type Session } from "./api";
import { melde } from "./attention";

type Verbindung = "verbindet" | "verbunden" | "getrennt";

// wischSchwelle ist die Strecke in Pixeln, ab der eine Berührung als Wischen gilt und
// nicht mehr als Tippen. Darunter bleibt die Geste beim Terminal, das dadurch den
// Fokus und auf dem Handy die Tastatur bekommt.
const wischSchwelle = 6;

// TerminalView hängt sich an eine Session, spielt den Scrollback zurück und schaltet
// danach live weiter. Ein Verbindungsabbruch führt zu einem Reconnect mit Backoff,
// der den Verlauf erneut lädt.
export function TerminalView({
  session,
  routerName,
  onBack,
  onRestart,
}: {
  session: Session;
  routerName: string;
  onBack: () => void;
  onRestart: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const terminal = useRef<Terminal | null>(null);
  const [verbindung, setVerbindung] = useState<Verbindung>("verbindet");
  const [status, setStatus] = useState<string>(session.status);
  const [hinweis, setHinweis] = useState<string | null>(null);
  const [exitCode, setExitCode] = useState<number | undefined>(session.exitCode);
  const [rueckfrage, setRueckfrage] = useState<string | null>(session.attention ?? null);
  const [amEnde, setAmEnde] = useState(true);

  useEffect(() => {
    if (!host.current) return;

    const term = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, "Cascadia Code", monospace',
      fontSize: 13,
      scrollback: 10000,
      theme: { background: "#000000" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host.current);
    fit.fit();
    terminal.current = term;
    // Nach dem Öffnen und nach jedem Wechsel liegt der Fokus im Terminal, sonst
    // liefen Tastenanschläge nach dem Umschalten ins Leere.
    term.focus();

    let ws: WebSocket | null = null;
    let abgemeldet = false;
    let versuch = 0;
    let reconnectTimer: number | undefined;
    let resizeTimer: number | undefined;

    const sendeGroesse = () => {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    };

    // pruefeEnde merkt sich, ob der Blick am unteren Rand klebt. Nur dann darf neue
    // Ausgabe den Ausschnitt mitziehen.
    const pruefeEnde = () => {
      const puffer = term.buffer.active;
      setAmEnde(puffer.viewportY >= puffer.baseY);
    };

    const verbinde = () => {
      setVerbindung(versuch === 0 ? "verbindet" : "getrennt");
      // Beim Reconnect wird der Verlauf komplett neu geladen, damit keine Lücke bleibt.
      term.reset();
      ws = new WebSocket(attachURL(session.id, term.cols, term.rows));
      ws.binaryType = "arraybuffer";

      ws.onopen = () => {
        versuch = 0;
        setVerbindung("verbunden");
        setHinweis(null);
        sendeGroesse();
        term.focus();
      };

      ws.onmessage = (event) => {
        if (typeof event.data !== "string") {
          term.write(new Uint8Array(event.data as ArrayBuffer));
          return;
        }
        const msg = JSON.parse(event.data);
        switch (msg.type) {
          case "status":
            setStatus(msg.status);
            if (msg.message) setHinweis(msg.message);
            break;
          case "truncated":
            setHinweis(msg.message ?? "Älterer Verlauf wurde abgeschnitten.");
            break;
          case "attention":
            // Eine leere Message nimmt die Meldung zurück.
            setRueckfrage(msg.message || null);
            if (msg.message && document.hidden) {
              // Im Vordergrund reicht das Banner; liegt die Seite im Hintergrund,
              // darf es auffälliger sein.
              melde(
                `${session.projectName ?? session.projectId} wartet`,
                msg.message,
                session.id,
              );
            }
            break;
          case "exit":
            setStatus(msg.status);
            setExitCode(msg.exitCode);
            setRueckfrage(null);
            term.write(
              `\r\n\x1b[90m— Session beendet (Status ${msg.status}${
                msg.exitCode === undefined ? "" : `, Exit-Code ${msg.exitCode}`
              }) —\x1b[0m\r\n`,
            );
            break;
        }
      };

      ws.onclose = () => {
        if (abgemeldet) return;
        setVerbindung("getrennt");
        versuch += 1;
        const wartezeit = Math.min(1000 * 2 ** (versuch - 1), 15000);
        reconnectTimer = window.setTimeout(() => {
          // Vor jedem Versuch nachsehen, ob die Session überhaupt noch läuft —
          // sonst wird endlos gegen eine beendete Session angeklopft.
          api
            .listSessions()
            .then((liste) => {
              if (abgemeldet) return;
              const aktuell = liste.find((s) => s.id === session.id);
              if (aktuell && aktuell.status !== "running") {
                setStatus(aktuell.status);
                setExitCode(aktuell.exitCode);
                setHinweis(null);
                return;
              }
              verbinde();
            })
            .catch(() => {
              // Auch der Router selbst kann weg sein: weiter mit Backoff versuchen.
              if (!abgemeldet) verbinde();
            });
        }, wartezeit);
      };

      ws.onerror = () => ws?.close();
    };

    const eingabe = term.onData((data) => {
      // Wer tippt, beantwortet die Rückfrage; der Server nimmt sie ebenfalls
      // zurück, aber der Hinweis soll nicht erst über den Umweg verschwinden.
      setRueckfrage(null);
      if (ws?.readyState !== WebSocket.OPEN) return;
      ws.send(new TextEncoder().encode(data));
    });

    const gescrollt = term.onScroll(pruefeEnde);
    const zeilenvorschub = term.onLineFeed(pruefeEnde);

    // Auf dem Handy gibt es kein Mausrad, und xterm.js übersetzt Berührungen nicht
    // von sich aus — ohne das Folgende ließe sich der Verlauf dort nicht ansehen.
    let letzteY = 0;
    let gewischt = 0;
    let rest = 0;

    const zeilenhoehe = () => {
      const hoehe = host.current?.clientHeight ?? 0;
      return term.rows > 0 && hoehe > 0 ? hoehe / term.rows : 17;
    };

    // blaettere macht aus einer Wischstrecke das, was am Desktop das Mausrad täte.
    // Welcher der drei Wege gilt, entscheidet die Anwendung im PTY, nicht wir:
    //
    //  1. Sie liest Mausereignisse (Claude Code tut das) — dann bekommt sie
    //     Radereignisse und scrollt ihre Ansicht selbst.
    //  2. Sie füllt den Alternate Screen ohne Maus (less, vim) — dort gibt es
    //     keinen Scrollback, wohl aber die Erwartung von Pfeiltasten.
    //  3. Sonst: gewöhnliche Ausgabe, der Scrollback von xterm.js trägt.
    //
    // Der alte Weg kannte nur 3. Genau deshalb ließ sich in Claude Code nichts
    // bewegen: dessen Alternate Screen hat keinen Scrollback, den man schieben
    // könnte — scrollLines() lief ins Leere.
    const blaettere = (zeilen: number) => {
      if (zeilen === 0) return;

      if (term.modes.mouseTrackingMode !== "none") {
        sendeRad(zeilen);
        return;
      }
      if (term.buffer.active.type === "alternate") {
        sendePfeile(zeilen);
        return;
      }
      term.scrollLines(zeilen);
    };

    // radschritt: so viele Zeilen entsprechen einer Raste des Mausrads.
    const radschritt = 3;

    // sendeRad schickt Radereignisse in SGR-Kodierung (CSI < Taste ; Spalte ; Zeile M).
    // Wer Maus-Tracking einschaltet, handelt heute immer auch SGR aus; eine
    // Anwendung, die es nicht kennt, verwirft die Sequenz, statt sie misszudeuten.
    const sendeRad = (zeilen: number) => {
      const hoch = zeilen < 0;
      const rasten = Math.max(1, Math.round(Math.abs(zeilen) / radschritt));
      // Die Position ist die Bildmitte: für das Rad wertet sie kaum jemand aus,
      // aber gültig muss sie sein.
      const spalte = Math.max(1, Math.min(term.cols, Math.ceil(term.cols / 2)));
      const zeile = Math.max(1, Math.min(term.rows, Math.ceil(term.rows / 2)));
      const taste = hoch ? 64 : 65;
      sende(`\x1b[<${taste};${spalte};${zeile}M`.repeat(rasten));
    };

    // sendePfeile ist der Weg für den Alternate Screen ohne Maus-Tracking — dasselbe,
    // was ein Terminalemulator dort aus dem Mausrad macht.
    const sendePfeile = (zeilen: number) => {
      const hoch = zeilen < 0;
      // Im Application-Cursor-Keys-Modus erwartet die Anwendung ESC O A statt ESC [ A.
      const praefix = term.modes.applicationCursorKeysMode ? "\x1bO" : "\x1b[";
      sende(`${praefix}${hoch ? "A" : "B"}`.repeat(Math.abs(zeilen)));
    };

    // sende geht bewusst direkt an den WebSocket, nicht über term.onData: Blättern
    // ist keine Antwort auf eine Rückfrage und darf deren Hinweis nicht löschen.
    const sende = (daten: string) => {
      if (ws?.readyState !== WebSocket.OPEN) return;
      ws.send(new TextEncoder().encode(daten));
    };

    const beruehrungStart = (event: TouchEvent) => {
      if (event.touches.length !== 1) return;
      letzteY = event.touches[0].clientY;
      gewischt = 0;
      rest = 0;
    };

    const beruehrungBewegt = (event: TouchEvent) => {
      if (event.touches.length !== 1) return;
      const y = event.touches[0].clientY;
      gewischt += Math.abs(y - letzteY);
      if (gewischt < wischSchwelle) {
        letzteY = y;
        return;
      }
      // Ab hier ist es ein Wischen: die Seite darf nicht mitwandern.
      event.preventDefault();
      rest += letzteY - y;
      letzteY = y;

      const zeilen = Math.trunc(rest / zeilenhoehe());
      if (zeilen !== 0) {
        rest -= zeilen * zeilenhoehe();
        blaettere(zeilen);
      }
    };

    // passive: false, sonst ignoriert der Browser preventDefault und scrollt die
    // ganze Seite statt des Terminals.
    host.current.addEventListener("touchstart", beruehrungStart, { passive: true });
    host.current.addEventListener("touchmove", beruehrungBewegt, { passive: false });

    const beobachter = new ResizeObserver(() => {
      window.clearTimeout(resizeTimer);
      // Debounced: erst wenn das Zoomen/Ziehen zur Ruhe kommt, wird gemeldet.
      resizeTimer = window.setTimeout(() => {
        fit.fit();
        sendeGroesse();
      }, 150);
    });
    beobachter.observe(host.current);

    verbinde();

    const element = host.current;
    return () => {
      abgemeldet = true;
      window.clearTimeout(reconnectTimer);
      window.clearTimeout(resizeTimer);
      element.removeEventListener("touchstart", beruehrungStart);
      element.removeEventListener("touchmove", beruehrungBewegt);
      beobachter.disconnect();
      eingabe.dispose();
      gescrollt.dispose();
      zeilenvorschub.dispose();
      ws?.close();
      term.dispose();
      terminal.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.id]);

  const beendet = status !== "running";

  return (
    <div className="terminal-view">
      <div className="terminal-head">
        <button onClick={onBack}>← Zurück</button>
        {beendet && (
          <button className="primary" onClick={onRestart}>
            Neue Session starten
          </button>
        )}
        <div className="grow">
          <span className="name">{session.projectName ?? session.projectId}</span>{" "}
          <span className="muted">· {session.runtimeId}</span>
        </div>
        <span className={`badge ${status}`}>
          {status}
          {exitCode === undefined ? "" : ` (${exitCode})`}
        </span>
        <span className="badge">{verbindung}</span>
      </div>
      {rueckfrage && (
        <div className="attention" role="status">
          <span className="attention-dot" aria-hidden="true" />
          <span className="grow">
            {routerName} meldet: wartet auf eine Entscheidung — <em>{rueckfrage}</em>
          </span>
          <button onClick={() => terminal.current?.focus()}>Antworten</button>
        </div>
      )}
      {hinweis && <div className="notice">{hinweis}</div>}
      <div className="terminal-wrap">
        <div className="terminal-host" ref={host} />
        {!amEnde && (
          <button
            className="zum-ende"
            onClick={() => {
              terminal.current?.scrollToBottom();
              terminal.current?.focus();
            }}
          >
            ↓ Ans Ende
          </button>
        )}
      </div>
    </div>
  );
}
