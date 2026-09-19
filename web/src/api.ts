// API-Client mit Token-Handling. Ein 401 wirft UnauthorizedError; die Oberfläche
// führt daraufhin zurück zum Token-Dialog.

const TOKEN_KEY = "project-router.token";

export class UnauthorizedError extends Error {
  constructor() {
    super("Token fehlt oder ist ungültig");
  }
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string): void {
  if (token) localStorage.setItem(TOKEN_KEY, token);
  else localStorage.removeItem(TOKEN_KEY);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init.body) headers.set("Content-Type", "application/json");

  const response = await fetch(path, { ...init, headers });
  if (response.status === 401) {
    clearToken();
    throw new UnauthorizedError();
  }
  if (response.status === 204) return undefined as T;
  const text = await response.text();
  const body = text ? JSON.parse(text) : undefined;
  if (!response.ok) {
    throw new ApiError(response.status, body?.error ?? `Fehler ${response.status}`);
  }
  return body as T;
}

/** Info beschreibt die Instanz: Anzeigename und ob die API ein Token verlangt. */
export interface Info {
  name: string;
  requiresToken: boolean;
}

export interface FsEntry {
  name: string;
  path: string;
  isGitRepo: boolean;
}

export interface FsListing {
  path: string;
  parent?: string;
  isRoots: boolean;
  entries: FsEntry[];
}

export interface Project {
  id: string;
  name: string;
  path: string;
  createdAt: string;
  runningSessions: number;
  available: boolean;
  isGitRepo: boolean;
}

export type RuntimeSource = "builtin" | "config" | "custom";

export interface Runtime {
  id: string;
  displayName: string;
  command: string;
  defaultArgs: string[] | null;
  env?: Record<string, string>;
  available: boolean;
  resolvedPath?: string;
  source?: RuntimeSource;
  /** true, wenn beim Entfernen eine ausgelieferte Definition wieder auflebt. */
  overridesStatic?: boolean;
  /** Kommando und Standardargumente als anzeigbare Zeile, z. B. "ollama launch claude". */
  commandLine?: string;
}

export interface RuntimeInput {
  id?: string;
  displayName: string;
  commandLine: string;
  env?: Record<string, string>;
}

export type SessionStatus = "running" | "exited" | "failed";

export interface Session {
  id: string;
  projectId: string;
  projectName?: string;
  runtimeId: string;
  args?: string[];
  status: SessionStatus;
  startedAt: string;
  endedAt?: string;
  exitCode?: number;
  error?: string;
  /** Offene Rückfrage des Agenten — leer oder fehlend, solange keine ansteht. */
  attention?: string;
}

export const api = {
  /** /api/info liegt vor der Token-Prüfung: der Name steht schon auf dem Token-Dialog. */
  info: () => request<Info>("/api/info"),

  listDirectories: (path?: string) =>
    request<FsListing>(`/api/fs${path ? `?path=${encodeURIComponent(path)}` : ""}`),

  listProjects: () =>
    request<{ projects: Project[] }>("/api/projects").then((r) => r.projects ?? []),

  createProject: (path: string, init: boolean, name?: string) =>
    request<Project>("/api/projects", {
      method: "POST",
      body: JSON.stringify({ path, init, name }),
    }),

  removeProject: (id: string) => request<void>(`/api/projects/${id}`, { method: "DELETE" }),

  listRuntimes: () =>
    request<{ runtimes: Runtime[] }>("/api/runtimes").then((r) => r.runtimes ?? []),

  saveRuntime: (input: RuntimeInput) =>
    request<Runtime>("/api/runtimes", { method: "POST", body: JSON.stringify(input) }),

  removeRuntime: (id: string) => request<void>(`/api/runtimes/${id}`, { method: "DELETE" }),

  listSessions: (projectId?: string) =>
    request<{ sessions: Session[] }>(
      `/api/sessions${projectId ? `?projectId=${encodeURIComponent(projectId)}` : ""}`,
    ).then((r) => r.sessions ?? []),

  startSession: (input: {
    projectId: string;
    runtimeId: string;
    args?: string[];
    cols?: number;
    rows?: number;
  }) => request<Session>("/api/sessions", { method: "POST", body: JSON.stringify(input) }),

  stopSession: (id: string) => request<Session>(`/api/sessions/${id}/stop`, { method: "POST" }),

  /** Startet eine frische Session mit denselben Eckdaten wie die angegebene. */
  restartSession: (id: string) =>
    request<Session>(`/api/sessions/${id}/restart`, { method: "POST" }),

  removeSession: (id: string) => request<void>(`/api/sessions/${id}`, { method: "DELETE" }),
};

// attachURL baut die WebSocket-URL; das Token läuft als Query-Parameter, weil die
// Browser-WebSocket-API keine Header setzen kann.
export function attachURL(sessionId: string, cols: number, rows: number): string {
  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const params = new URLSearchParams({ cols: String(cols), rows: String(rows) });
  const token = getToken();
  if (token) params.set("token", token);
  return `${protocol}//${location.host}/api/sessions/${sessionId}/attach?${params}`;
}
