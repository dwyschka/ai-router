import { useState } from "react";
import { setToken } from "./api";

// TokenDialog ist der Einstieg, wenn der Router ein Token verlangt.
export function TokenDialog({ onDone }: { onDone: () => void }) {
  const [value, setValue] = useState("");

  return (
    <div className="backdrop">
      <form
        className="dialog"
        onSubmit={(e) => {
          e.preventDefault();
          setToken(value.trim());
          onDone();
        }}
      >
        <header>Token benötigt</header>
        <div className="body">
          <p className="muted">
            Der Router verlangt ein Token. Es wird im Browser abgelegt und bei jedem Request
            mitgeschickt.
          </p>
          <div className="field">
            <label htmlFor="token">Token</label>
            <input
              id="token"
              type="password"
              autoFocus
              value={value}
              onChange={(e) => setValue(e.target.value)}
            />
          </div>
        </div>
        <footer>
          <button className="primary" type="submit" disabled={!value.trim()}>
            Verbinden
          </button>
        </footer>
      </form>
    </div>
  );
}
