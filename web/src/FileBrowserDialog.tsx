import { useCallback, useEffect, useState } from "react";
import { api, type FsEntry, type FsListing } from "./api";

// FileBrowserDialog navigiert durch die freigegebenen Roots und wählt einen Pfad für
// ein neues Projekt aus. Bestehende Repositories sind markiert; ein Verzeichnis ohne
// Repository lässt sich nur mit ausdrücklicher Initialisierung übernehmen.
export function FileBrowserDialog({
  onClose,
  onPick,
}: {
  onClose: () => void;
  onPick: (path: string, init: boolean) => Promise<void>;
}) {
  const [listing, setListing] = useState<FsListing | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [neuerOrdner, setNeuerOrdner] = useState("");

  const load = useCallback(async (path?: string) => {
    setError(null);
    try {
      setListing(await api.listDirectories(path));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const aktuell = listing?.path ?? "";
  const istRepo = listing?.entries.some((e) => e.path === aktuell && e.isGitRepo) ?? false;

  async function uebernehmen(path: string, init: boolean) {
    setBusy(true);
    setError(null);
    try {
      await onPick(path, init);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  }

  return (
    <div className="backdrop">
      <div className="dialog">
        <header>Projektpfad wählen</header>
        <div className="body">
          {listing?.isRoots ? (
            <div className="browser-current">Freigegebene Wurzelverzeichnisse</div>
          ) : (
            <div className="browser-current">{aktuell}</div>
          )}
          {error && <p className="error">{error}</p>}

          <div className="list">
            {listing && !listing.isRoots && (
              <button className="row" onClick={() => void load(listing.parent)}>
                <span className="browser-entry">
                  <span aria-hidden>↰</span>
                  <span className="grow">{listing.parent ? "Eine Ebene höher" : "Zurück zu den Roots"}</span>
                </span>
              </button>
            )}
            {listing?.entries.length === 0 && <div className="empty">Keine Unterverzeichnisse</div>}
            {listing?.entries.map((entry: FsEntry) => (
              <div className="row" key={entry.path}>
                <button
                  className="browser-entry grow"
                  onClick={() => void load(entry.path)}
                  style={{ background: "none", border: "none", padding: 0 }}
                >
                  <span aria-hidden>📁</span>
                  <span className="grow">
                    <span className="name">{entry.name}</span>
                    <div className="path">{entry.path}</div>
                  </span>
                </button>
                {entry.isGitRepo && <span className="badge git">Git-Repository</span>}
                <button
                  disabled={busy}
                  onClick={() => void uebernehmen(entry.path, !entry.isGitRepo)}
                  title={entry.isGitRepo ? "Als Projekt übernehmen" : "Initialisieren und übernehmen"}
                >
                  {entry.isGitRepo ? "Übernehmen" : "Init + übernehmen"}
                </button>
              </div>
            ))}
          </div>

          {listing && !listing.isRoots && (
            <div className="field" style={{ marginTop: 18 }}>
              <label htmlFor="neuer-ordner">Neues Verzeichnis hier anlegen und initialisieren</label>
              <div style={{ display: "flex", gap: 8 }}>
                <input
                  id="neuer-ordner"
                  placeholder="name-des-projekts"
                  value={neuerOrdner}
                  onChange={(e) => setNeuerOrdner(e.target.value)}
                />
                <button
                  disabled={busy || !neuerOrdner.trim()}
                  onClick={() => void uebernehmen(`${aktuell}/${neuerOrdner.trim()}`, true)}
                >
                  Anlegen
                </button>
              </div>
            </div>
          )}
        </div>
        <footer>
          {listing && !listing.isRoots && (
            <button
              className="primary"
              disabled={busy}
              onClick={() => void uebernehmen(aktuell, !istRepo)}
            >
              Dieses Verzeichnis übernehmen
            </button>
          )}
          <button onClick={onClose} disabled={busy}>
            Abbrechen
          </button>
        </footer>
      </div>
    </div>
  );
}
