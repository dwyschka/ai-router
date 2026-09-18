import { useEffect, useState } from "react";
import { api, type Project, type Runtime } from "./api";

// StartSessionDialog wählt die Runtime für eine neue Session. Nicht verfügbare
// Runtimes sind ausgegraut und nennen das fehlende Kommando.
export function StartSessionDialog({
  project,
  onClose,
  onStart,
}: {
  project: Project;
  onClose: () => void;
  onStart: (runtimeId: string, args: string[]) => Promise<void>;
}) {
  const [runtimes, setRuntimes] = useState<Runtime[] | null>(null);
  const [runtimeId, setRuntimeId] = useState("");
  const [args, setArgs] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .listRuntimes()
      .then((list) => {
        setRuntimes(list);
        setRuntimeId(list.find((r) => r.available)?.id ?? "");
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const gewaehlt = runtimes?.find((r) => r.id === runtimeId);

  async function starten() {
    setBusy(true);
    setError(null);
    try {
      await onStart(
        runtimeId,
        args
          .split(" ")
          .map((a) => a.trim())
          .filter(Boolean),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  }

  return (
    <div className="backdrop">
      <div className="dialog">
        <header>Session starten — {project.name}</header>
        <div className="body">
          {error && <p className="error">{error}</p>}
          <div className="field">
            <label htmlFor="runtime">Runtime</label>
            <select id="runtime" value={runtimeId} onChange={(e) => setRuntimeId(e.target.value)}>
              {runtimes?.map((r) => (
                <option key={r.id} value={r.id} disabled={!r.available}>
                  {r.displayName}
                  {r.available ? "" : ` — ${r.command} nicht im PATH`}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="args">Zusätzliche Argumente (optional, durch Leerzeichen getrennt)</label>
            <input id="args" value={args} onChange={(e) => setArgs(e.target.value)} />
          </div>
          <p className="muted">
            Arbeitsverzeichnis: <span className="path">{project.path}</span>
          </p>
        </div>
        <footer>
          <button
            className="primary"
            disabled={busy || !gewaehlt?.available}
            onClick={() => void starten()}
          >
            Starten
          </button>
          <button onClick={onClose} disabled={busy}>
            Abbrechen
          </button>
        </footer>
      </div>
    </div>
  );
}
