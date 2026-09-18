import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { api, attachURL, type Session } from "./api";

type Verbindung = "verbindet" | "verbunden" | "getrennt";

// TerminalView hängt sich an eine Session, spielt den Scrollback zurück und schaltet
// danach live weiter. Ein Verbindungsabbruch führt zu einem Reconnect mit Backoff,
// der den Verlauf erneut lädt.
export function TerminalView({
  session,
  onBack,
  onRestart,
}: {
  session: Session;
  onBack: () => void;
  onRestart: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const [verbindung, setVerbindung] = useState<Verbindung>("verbindet");
  const [status, setStatus] = useState<string>(session.status);
  const [hinweis, setHinweis] = useState<string | null>(null);
  const [exitCode, setExitCode] = useState<number | undefined>(session.exitCode);

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
          case "exit":
            setStatus(msg.status);
            setExitCode(msg.exitCode);
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
      if (ws?.readyState !== WebSocket.OPEN) return;
      ws.send(new TextEncoder().encode(data));
    });

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

    return () => {
      abgemeldet = true;
      window.clearTimeout(reconnectTimer);
      window.clearTimeout(resizeTimer);
      beobachter.disconnect();
      eingabe.dispose();
      ws?.close();
      term.dispose();
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
      {hinweis && <div className="notice">{hinweis}</div>}
      <div className="terminal-host" ref={host} />
    </div>
  );
}
