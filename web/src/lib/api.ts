import type {
  AuthMe,
  ChapterView,
  Config,
  CreateUserRequest,
  IndexEntry,
  Meta,
  Progress,
  PublicUser,
  Role,
  StreamEvent,
  VersionInfo,
} from "@ao3hub/shared";

const base = "/api";

export class HttpError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(base + path, {
    credentials: "same-origin",
    headers: { "content-type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    let msg: string;
    try {
      const body = await res.json();
      msg = body?.error ?? JSON.stringify(body);
    } catch {
      msg = await res.text();
    }
    throw new HttpError(res.status, msg || `${res.status}`);
  }
  return (await res.json()) as T;
}

export type StoriesListResponse = { stories: IndexEntry[] };
export type StoryDetail = { meta: Meta; progress: Progress };

export const api = {
  listStories: () => http<StoriesListResponse>("/stories"),
  getStory: (id: string) => http<StoryDetail>(`/stories/${id}`),
  getChapter: (id: string, n: number) =>
    http<ChapterView>(`/stories/${id}/chapters/${n}`),
  createFromUrl: (url: string) =>
    http<{ id: string; status: string }>("/stories", {
      method: "POST",
      body: JSON.stringify({ url }),
    }),
  uploadHtml: async (file: File | string) => {
    const form = new FormData();
    if (typeof file === "string") {
      form.append("file", new Blob([file], { type: "text/html" }), "upload.html");
    } else {
      form.append("file", file);
    }
    const res = await fetch(base + "/stories/upload", {
      method: "POST",
      credentials: "same-origin",
      body: form,
    });
    if (!res.ok) {
      let msg: string;
      try {
        const body = await res.json();
        msg = body?.error ?? JSON.stringify(body);
      } catch {
        msg = await res.text();
      }
      throw new HttpError(res.status, msg || `${res.status}`);
    }
    return (await res.json()) as { id: string; status: string };
  },
  retry: (id: string, body: { blockIds?: string[]; chapterIndex?: number } = {}) =>
    http<{ ok: true }>(`/stories/${id}/retry`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  remove: (id: string) =>
    http<{ ok: true }>(`/stories/${id}`, { method: "DELETE" }),

  getConfig: () => http<Config & { llm: Config["llm"] & { hasApiKey: boolean }; ao3: Config["ao3"] & { hasCookie: boolean } }>("/config"),
  saveConfig: (body: any) =>
    http<{ ok: true }>("/config", { method: "PUT", body: JSON.stringify(body) }),
  testConfig: () =>
    http<{ ok: boolean; content?: string; error?: string }>("/config/test", {
      method: "POST",
    }),

  version: () => http<VersionInfo>("/update/version"),
  checkUpdate: () =>
    http<VersionInfo>("/update/check", { method: "POST" }),
  applyUpdate: (force = false) =>
    http<{ ok: boolean; message: string; version?: string }>("/update/apply", {
      method: "POST",
      body: JSON.stringify({ force }),
    }),

  me: () => http<AuthMe>("/auth/me"),
  login: (username: string, password: string) =>
    http<{ user: PublicUser }>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => http<{ ok: true }>("/auth/logout", { method: "POST" }),
  setup: (username: string, password: string) =>
    http<{ user: PublicUser }>("/auth/setup", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),

  listUsers: () => http<{ users: PublicUser[] }>("/users"),
  createUser: (body: CreateUserRequest) =>
    http<{ user: PublicUser }>("/users", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateUser: (id: string, body: { password?: string; role?: Role }) =>
    http<{ user: PublicUser }>(`/users/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  deleteUser: (id: string) =>
    http<{ ok: true }>(`/users/${id}`, { method: "DELETE" }),
};

export function subscribeStream(
  id: string,
  onEvent: (e: StreamEvent) => void,
  onError?: (e: Event) => void,
): () => void {
  const es = new EventSource(`${base}/stories/${id}/stream`);
  const types = ["progress", "phase", "block-done", "block-error", "chapter-done"] as const;
  for (const t of types) {
    es.addEventListener(t, (raw) => {
      try {
        onEvent(JSON.parse((raw as MessageEvent).data) as StreamEvent);
      } catch {}
    });
  }
  if (onError) es.onerror = onError;
  return () => es.close();
}
