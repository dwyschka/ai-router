import type { Session } from "./api";

// SessionSwitcher ist die Leiste über dem Terminal: ein Eintrag je Session, Klick
// wechselt sofort. Laufende Sessions stehen vorn, die gerade offene ist markiert.
export function SessionSwitcher({
  sessions,
  activeId,
  onSelect,
}: {
  sessions: Session[];
  activeId: string;
  onSelect: (session: Session) => void;
}) {
  if (sessions.length < 2) return null;

  return (
    <div className="switcher" role="tablist" aria-label="Offene Sessions">
      {sessions.map((s, index) => (
        <button
          key={s.id}
          role="tab"
          aria-selected={s.id === activeId}
          className={`switcher-tab ${s.id === activeId ? "active" : ""} ${
            s.attention ? "wartet" : ""
          }`}
          onClick={() => onSelect(s)}
          title={`${s.projectName ?? s.projectId} · ${s.runtimeId}${
            index < 9 ? ` — Ctrl+Alt+${index + 1}` : ""
          }${s.attention ? ` — wartet: ${s.attention}` : ""}`}
        >
          <span className={`dot ${s.attention ? "wartet" : s.status}`} aria-hidden />
          <span className="switcher-name">{s.projectName ?? s.projectId}</span>
          <span className="switcher-runtime">{s.runtimeId}</span>
          {index < 9 && <span className="switcher-key">{index + 1}</span>}
        </button>
      ))}
    </div>
  );
}

// schnellwahl sortiert die Sessions für die Leiste: laufende zuerst, darin die
// zuletzt gestarteten oben. Die gerade offene Session ist immer enthalten, auch wenn
// sie bereits beendet ist.
export function schnellwahl(sessions: Session[], activeId: string): Session[] {
  // Laufende Sessions plus die gerade offene — auch wenn diese bereits beendet ist.
  return sessions
    .filter((s) => s.status === "running" || s.id === activeId)
    .sort((a, b) => {
      if (a.status !== b.status) return a.status === "running" ? -1 : 1;
      return a.startedAt.localeCompare(b.startedAt);
    });
}
