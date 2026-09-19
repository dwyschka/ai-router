import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  getToken,
  UnauthorizedError,
  type Project,
  type Runtime,
  type Session,
} from "./api";
import {
  bereiteVor,
  erlaubnis,
  frageErlaubnis,
  melde,
  setzeTitelmarke,
  type Erlaubnis,
} from "./attention";
import { TokenDialog } from "./TokenDialog";
import { FileBrowserDialog } from "./FileBrowserDialog";
import { StartSessionDialog } from "./StartSessionDialog";
import { TerminalView } from "./TerminalView";
import { SessionSwitcher, schnellwahl } from "./SessionSwitcher";
import { RuntimeDialog } from "./RuntimeDialog";

// herkunft benennt, woher eine Runtime-Definition stammt.
function herkunft(r: Runtime): string {
  switch (r.source) {
    case "custom":
      return "hinterlegt";
    case "config":
      return "Konfiguration";
    default:
      return "ausgeliefert";
  }
}

// startSession liest die Session aus der Adresse: eine Benachrichtigung verlinkt
// direkt auf die Session, die auf eine Entscheidung wartet.
function sessionAusAdresse(): string | null {
  return new URLSearchParams(location.search).get("session");
}

export function App() {
  const [tokenNoetig, setTokenNoetig] = useState(false);
  const [name, setName] = useState("project-router");
  const [projekte, setProjekte] = useState<Project[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [runtimes, setRuntimes] = useState<Runtime[]>([]);
  const [runtimeDialog, setRuntimeDialog] = useState<{ vorhanden?: Runtime } | null>(null);
  const [fehler, setFehler] = useState<string | null>(null);
  const [browserOffen, setBrowserOffen] = useState(false);
  const [startFuer, setStartFuer] = useState<Project | null>(null);
  const [offeneSessionId, setOffeneSessionId] = useState<string | null>(sessionAusAdresse);
  const [meldeErlaubnis, setMeldeErlaubnis] = useState<Erlaubnis>(erlaubnis);
  // gemeldet hält fest, für welche Sessions schon eine Meldung rausging — sonst
  // benachrichtigt jeder Durchlauf der Liste erneut.
  const gemeldet = useRef<Set<string>>(new Set());

  // Die offene Session wird aus der Liste gelesen, damit Statuswechsel und das
  // Wegfallen einer Session direkt durchschlagen.
  const offeneSession = sessions.find((s) => s.id === offeneSessionId) ?? null;
  const umschaltbar = offeneSession ? schnellwahl(sessions, offeneSession.id) : [];

  const laden = useCallback(async () => {
    try {
      const [p, s, r] = await Promise.all([
        api.listProjects(),
        api.listSessions(),
        api.listRuntimes(),
      ]);
      setProjekte(p);
      setSessions(s);
      setRuntimes(r);
      setFehler(null);
      setTokenNoetig(false);
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        setTokenNoetig(true);
        return;
      }
      setFehler(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void laden();
  }, [laden]);

  // Der Name der Instanz steht in der Konfiguration des Routers; /api/info liegt
  // vor der Token-Prüfung und ist deshalb auch auf dem Token-Dialog schon da.
  useEffect(() => {
    api
      .info()
      .then((info) => setName(info.name))
      .catch(() => undefined);
  }, []);

  // Den Service Worker früh registrieren: ohne ihn zeigt Chrome auf Android keine
  // Benachrichtigung an, und bis er bereit ist, vergeht ein Moment.
  useEffect(() => {
    void bereiteVor();
  }, []);

  // Übersicht und Session-Leiste halten sich aktuell.
  useEffect(() => {
    if (tokenNoetig) return;
    const timer = window.setInterval(() => void laden(), 4000);
    return () => window.clearInterval(timer);
  }, [laden, tokenNoetig]);

  // Rückfragen, die nicht gerade offen auf dem Schirm stehen, werden gemeldet: das
  // Banner der Terminalansicht sieht nur, wer ohnehin hinschaut.
  useEffect(() => {
    const offen = sessions.filter((s) => s.attention && s.status === "running");
    for (const s of offen) {
      if (gemeldet.current.has(s.id) || s.id === offeneSessionId) continue;
      gemeldet.current.add(s.id);
      melde(`${s.projectName ?? s.projectId} wartet`, s.attention!, s.id);
    }
    // Beantwortete Rückfragen dürfen später wieder melden.
    for (const id of [...gemeldet.current]) {
      if (!offen.some((s) => s.id === id)) gemeldet.current.delete(id);
    }
    setzeTitelmarke(name, offen.length);
  }, [sessions, offeneSessionId, name]);

  // Tastaturkürzel zum Wechseln: Ctrl+Alt+1…9 springt direkt, Ctrl+Alt+←/→ blättert.
  // Die Kürzel laufen in der Capture-Phase, damit das Terminal sie nicht vorher
  // verschluckt; alles andere geht unverändert an das PTY.
  useEffect(() => {
    if (!offeneSession || umschaltbar.length < 2) return;

    const handler = (event: KeyboardEvent) => {
      if (!event.ctrlKey || !event.altKey || event.metaKey) return;

      const aktuell = umschaltbar.findIndex((s) => s.id === offeneSession.id);
      let ziel = -1;

      const ziffer = /^Digit([1-9])$/.exec(event.code);
      if (ziffer) {
        ziel = Number(ziffer[1]) - 1;
      } else if (event.code === "ArrowRight" || event.code === "BracketRight") {
        ziel = (aktuell + 1) % umschaltbar.length;
      } else if (event.code === "ArrowLeft" || event.code === "BracketLeft") {
        ziel = (aktuell - 1 + umschaltbar.length) % umschaltbar.length;
      }

      if (ziel < 0 || ziel >= umschaltbar.length) return;
      event.preventDefault();
      event.stopPropagation();
      setOffeneSessionId(umschaltbar[ziel].id);
    };

    window.addEventListener("keydown", handler, true);
    return () => window.removeEventListener("keydown", handler, true);
  }, [offeneSession, umschaltbar]);

  // oeffneSession führt nie in eine Sackgasse: eine beendete Session wird mit
  // denselben Eckdaten frisch gestartet und die neue geöffnet.
  async function oeffneSession(s: Session) {
    if (s.status === "running") {
      setOffeneSessionId(s.id);
      return;
    }
    try {
      const frisch = await api.restartSession(s.id);
      await laden();
      setOffeneSessionId(frisch.id);
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        setTokenNoetig(true);
        return;
      }
      setFehler(err instanceof Error ? err.message : String(err));
    }
  }

  async function fuehreAus(aktion: () => Promise<unknown>) {
    try {
      await aktion();
      await laden();
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        setTokenNoetig(true);
        return;
      }
      setFehler(err instanceof Error ? err.message : String(err));
    }
  }

  if (tokenNoetig) {
    return <TokenDialog routerName={name} onDone={() => void laden()} />;
  }

  if (offeneSession) {
    return (
      <div className="app terminal-page">
        <SessionSwitcher
          sessions={umschaltbar}
          activeId={offeneSession.id}
          onSelect={(s) => setOffeneSessionId(s.id)}
        />
        <TerminalView
          key={`${offeneSession.id}:${offeneSession.startedAt}`}
          session={offeneSession}
          routerName={name}
          onRestart={() => void oeffneSession(offeneSession)}
          onBack={() => {
            setOffeneSessionId(null);
            void laden();
          }}
        />
      </div>
    );
  }

  return (
    <div className="app">
      <header className="app-header">
        <div>
          <h1>{name}</h1>
          <div className="sub">Agent-Sessions laufen serverseitig weiter — der Browser hängt sich nur an.</div>
        </div>
        {meldeErlaubnis === "offen" && (
          <button
            onClick={() => {
              void frageErlaubnis().then(setMeldeErlaubnis);
            }}
            title="Meldet, wenn ein Agent auf eine Entscheidung wartet"
          >
            Benachrichtigungen erlauben
          </button>
        )}
        {meldeErlaubnis === "unsicherer-kontext" && (
          <span className="badge warnung" title={`Der Browser erlaubt Benachrichtigungen nur über HTTPS oder auf localhost — diese Seite läuft über ${location.protocol}//. Bis dahin meldet sich ${name} hier in der Oberfläche, und per Webhook auch außerhalb.`}>
            Benachrichtigungen brauchen HTTPS
          </span>
        )}
        {getToken() && (
          <button
            onClick={() => {
              api.listProjects().catch(() => undefined);
              localStorage.removeItem("project-router.token");
              setTokenNoetig(true);
            }}
          >
            Token vergessen
          </button>
        )}
      </header>

      {fehler && <p className="error">{fehler}</p>}

      <section className="section">
        <div className="section-head">
          <h2>Projekte</h2>
          <button className="primary" onClick={() => setBrowserOffen(true)}>
            Projekt hinzufügen
          </button>
        </div>
        <div className="list">
          {projekte.length === 0 && <div className="card empty">Noch keine Projekte registriert.</div>}
          {projekte.map((p) => (
            <div className="row" key={p.id}>
              <div className="grow">
                <div className="name">{p.name}</div>
                <div className="path">{p.path}</div>
              </div>
              {!p.available && <span className="badge offline">Pfad fehlt</span>}
              <span className="badge">
                {p.runningSessions} {p.runningSessions === 1 ? "Session" : "Sessions"}
              </span>
              <button disabled={!p.available} onClick={() => setStartFuer(p)}>
                Session starten
              </button>
              <button className="danger" onClick={() => void fuehreAus(() => api.removeProject(p.id))}>
                Entfernen
              </button>
            </div>
          ))}
        </div>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Sessions</h2>
        </div>
        <div className="list">
          {sessions.length === 0 && <div className="card empty">Keine Sessions.</div>}
          {sessions.map((s) => (
            <div className="row" key={s.id}>
              <div className="grow">
                <div className="name">
                  {s.projectName ?? s.projectId} <span className="muted">· {s.runtimeId}</span>
                </div>
                <div className="path">
                  gestartet {new Date(s.startedAt).toLocaleString()}
                  {s.endedAt ? ` · beendet ${new Date(s.endedAt).toLocaleString()}` : ""}
                  {s.exitCode === undefined ? "" : ` · Exit-Code ${s.exitCode}`}
                </div>
              </div>
              {s.attention && (
                <span className="badge attention-badge" title={s.attention}>
                  wartet auf Entscheidung
                </span>
              )}
              <span className={`badge ${s.status}`}>{s.status}</span>
              <button onClick={() => void oeffneSession(s)}>
                {s.status === "running" ? "Öffnen" : "Neu starten"}
              </button>
              {s.status === "running" ? (
                <button className="danger" onClick={() => void fuehreAus(() => api.stopSession(s.id))}>
                  Beenden
                </button>
              ) : (
                <button className="danger" onClick={() => void fuehreAus(() => api.removeSession(s.id))}>
                  Entfernen
                </button>
              )}
            </div>
          ))}
        </div>
      </section>

      <section className="section">
        <div className="section-head">
          <h2>Runtimes</h2>
          <button onClick={() => setRuntimeDialog({})}>Eigene Runtime hinterlegen</button>
        </div>
        <div className="list">
          {runtimes.map((r) => (
            <div className="row" key={r.id}>
              <div className="grow">
                <div className="name">
                  {r.displayName} <span className="muted">· {r.id}</span>
                </div>
                <div className="path">
                  {r.commandLine ?? r.command}
                  {r.available ? "" : ` — ${r.command} nicht im PATH`}
                </div>
              </div>
              <span className={`badge ${r.available ? "running" : "offline"}`}>
                {r.available ? "verfügbar" : "fehlt"}
              </span>
              <span className="badge">{herkunft(r)}</span>
              <button onClick={() => setRuntimeDialog({ vorhanden: r })}>
                {r.source === "custom" ? "Bearbeiten" : "Überschreiben"}
              </button>
              {r.source === "custom" && (
                <button className="danger" onClick={() => void fuehreAus(() => api.removeRuntime(r.id))}>
                  {r.overridesStatic ? "Zurücksetzen" : "Entfernen"}
                </button>
              )}
            </div>
          ))}
        </div>
      </section>

      {runtimeDialog && (
        <RuntimeDialog
          vorhanden={runtimeDialog.vorhanden}
          onClose={() => setRuntimeDialog(null)}
          onSaved={() => {
            setRuntimeDialog(null);
            void laden();
          }}
        />
      )}

      {browserOffen && (
        <FileBrowserDialog
          onClose={() => setBrowserOffen(false)}
          onPick={async (path, init) => {
            await api.createProject(path, init);
            setBrowserOffen(false);
            await laden();
          }}
        />
      )}

      {startFuer && (
        <StartSessionDialog
          project={startFuer}
          onClose={() => setStartFuer(null)}
          onStart={async (runtimeId, args) => {
            const session = await api.startSession({
              projectId: startFuer.id,
              runtimeId,
              args,
              cols: 80,
              rows: 24,
            });
            setStartFuer(null);
            await laden();
            setOffeneSessionId(session.id);
          }}
        />
      )}
    </div>
  );
}
