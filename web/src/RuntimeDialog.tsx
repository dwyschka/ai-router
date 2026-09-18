import { useState } from "react";
import { api, type Runtime } from "./api";

// RuntimeDialog hinterlegt ein eigenes Startkommando. Die Zeile wird serverseitig
// ohne Shell in einen Argumentvektor zerlegt — `ollama launch claude` startet also
// `ollama` mit den Argumenten `launch` und `claude`.
export function RuntimeDialog({
  vorhanden,
  onClose,
  onSaved,
}: {
  vorhanden?: Runtime;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(vorhanden?.displayName ?? "");
  const [id, setId] = useState(vorhanden?.id ?? "");
  const [commandLine, setCommandLine] = useState(vorhanden?.commandLine ?? "");
  const [env, setEnv] = useState(
    Object.entries(vorhanden?.env ?? {})
      .map(([k, v]) => `${k}=${v}`)
      .join("\n"),
  );
  const [fehler, setFehler] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function speichern() {
    setBusy(true);
    setFehler(null);
    try {
      const variablen: Record<string, string> = {};
      for (const zeile of env.split("\n")) {
        const trimmed = zeile.trim();
        if (!trimmed) continue;
        const index = trimmed.indexOf("=");
        if (index < 1) {
          throw new Error(`Umgebungsvariable ohne "=": ${trimmed}`);
        }
        variablen[trimmed.slice(0, index).trim()] = trimmed.slice(index + 1);
      }
      await api.saveRuntime({
        id: id.trim() || undefined,
        displayName: name.trim(),
        commandLine: commandLine.trim(),
        env: variablen,
      });
      onSaved();
    } catch (err) {
      setFehler(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  }

  return (
    <div className="backdrop">
      <div className="dialog">
        <header>{vorhanden ? `Runtime bearbeiten — ${vorhanden.id}` : "Eigene Runtime hinterlegen"}</header>
        <div className="body">
          {fehler && <p className="error">{fehler}</p>}

          <div className="field">
            <label htmlFor="rt-name">Anzeigename</label>
            <input
              id="rt-name"
              autoFocus
              placeholder="Claude über Ollama"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="field">
            <label htmlFor="rt-command">Startkommando</label>
            <input
              id="rt-command"
              placeholder="ollama launch claude"
              value={commandLine}
              onChange={(e) => setCommandLine(e.target.value)}
              style={{ fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" }}
            />
            <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              Wird ohne Shell ausgeführt: das erste Wort ist das Kommando (muss im PATH
              liegen), der Rest sind Standardargumente. Argumente mit Leerzeichen in
              Anführungszeichen setzen.
            </p>
          </div>

          <div className="field">
            <label htmlFor="rt-id">
              Kennung {vorhanden ? "" : "(optional — leer: aus dem Namen abgeleitet)"}
            </label>
            <input
              id="rt-id"
              placeholder="claude-ollama"
              value={id}
              disabled={!!vorhanden}
              onChange={(e) => setId(e.target.value)}
            />
            <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              Eine bestehende Kennung wie <code>claude-code</code> überschreibt die
              ausgelieferte Definition; beim Entfernen lebt sie wieder auf.
            </p>
          </div>

          <div className="field">
            <label htmlFor="rt-env">Umgebungsvariablen (optional, eine je Zeile: NAME=Wert)</label>
            <textarea
              id="rt-env"
              rows={3}
              value={env}
              onChange={(e) => setEnv(e.target.value)}
              style={{ fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" }}
            />
          </div>
        </div>
        <footer>
          <button className="primary" disabled={busy || !commandLine.trim()} onClick={() => void speichern()}>
            Speichern
          </button>
          <button onClick={onClose} disabled={busy}>
            Abbrechen
          </button>
        </footer>
      </div>
    </div>
  );
}
