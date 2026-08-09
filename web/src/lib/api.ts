import { z } from "zod";
import * as Schema from "@ao3hub/shared";
import type {
  AuthMe,
  ChapterView,
  Config,
  CreateUserRequest,
  Meta,
  Progress,
  PublicUser,
  Role,
  StoryList,
  StreamEvent,
  TranslationMode,
  TranslationStatusView,
  VersionInfo,
  ApplyUpdateRequest,
} from "@ao3hub/shared";

const base = "/api";
export const AUTH_INVALID_EVENT = "ao3hub:auth-invalid";
let authInvalidationPending = false;
let authStateEpoch = 0;

const StoryDetailSchema = z.object({
  meta: Schema.Meta,
  progress: Schema.Progress,
});
const ConfigResponseSchema = Schema.Config.extend({
  llm: Schema.LlmConfig.extend({ hasApiKey: z.boolean() }),
  ao3: Schema.Ao3Config.extend({ hasCookie: z.boolean() }),
});
const PublicConfigSchema = z.object({
  reader: Schema.ReaderConfig,
  ui: Schema.UiConfig,
  llm: z.object({ mode: Schema.TranslationMode }),
});
export class HttpError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "HttpError";
    this.status = status;
  }
}

export function markAuthStateFresh(): void {
  authStateEpoch += 1;
  authInvalidationPending = false;
}

export function isAuthStateEpochCurrent(epoch: unknown): boolean {
  return typeof epoch === "number" && epoch === authStateEpoch;
}

function notifyAuthInvalid(path: string, requestEpoch: number): void {
  if (
    path === "/auth/login" ||
    path === "/auth/setup" ||
    requestEpoch !== authStateEpoch ||
    authInvalidationPending
  ) {
    return;
  }
  authInvalidationPending = true;
  window.dispatchEvent(
    new CustomEvent(AUTH_INVALID_EVENT, { detail: requestEpoch }),
  );
}

async function responseBody(res: Response): Promise<unknown> {
  const text = await res.text();
  if (!text) return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return text;
  }
}

function errorMessage(body: unknown, status: number): string {
  if (typeof body === "string") return body || String(status);
  if (body && typeof body === "object" && "error" in body) {
    const error = (body as { error?: unknown }).error;
    if (typeof error === "string" && error) return error;
  }
  return body == null ? String(status) : JSON.stringify(body);
}

async function parseResponse<T>(
  res: Response,
  path: string,
  schema?: z.ZodType<T, z.ZodTypeDef, unknown>,
  requestEpoch = authStateEpoch,
): Promise<T> {
  const body = await responseBody(res);
  if (!res.ok) {
    if (res.status === 401) notifyAuthInvalid(path, requestEpoch);
    throw new HttpError(res.status, errorMessage(body, res.status));
  }
  if (!schema) return body as T;
  const parsed = schema.safeParse(body);
  if (!parsed.success) {
    throw new Error(
      `API response schema mismatch for ${path}: ${parsed.error.message}`,
    );
  }
  return parsed.data;
}

async function http<T>(
  path: string,
  init?: RequestInit,
  schema?: z.ZodType<T, z.ZodTypeDef, unknown>,
): Promise<T> {
  const requestEpoch = authStateEpoch;
  const res = await fetch(base + path, {
    ...init,
    credentials: "same-origin",
    headers: { "content-type": "application/json", ...(init?.headers ?? {}) },
  });
  return parseResponse(res, path, schema, requestEpoch);
}

const pathSegment = (value: string | number) =>
  encodeURIComponent(String(value));

export type StoriesListResponse = StoryList;
export type StoryDetail = { meta: Meta; progress: Progress };

export const api = {
  listStories: (signal?: AbortSignal) =>
    http<StoriesListResponse>("/stories", { signal }, Schema.StoryList),
  getStory: (id: string, signal?: AbortSignal) =>
    http<StoryDetail>(
      `/stories/${pathSegment(id)}`,
      { signal },
      StoryDetailSchema,
    ),
  getChapter: (id: string, n: number, signal?: AbortSignal) =>
    http<ChapterView>(
      `/stories/${pathSegment(id)}/chapters/${pathSegment(n)}`,
      { signal },
      Schema.ChapterView,
    ),
  createFromUrl: (url: string, mode?: TranslationMode) =>
    http<{ id: string; status: string }>("/stories", {
      method: "POST",
      body: JSON.stringify(mode ? { url, mode } : { url }),
    }),
  uploadHtml: async (file: File | string, mode?: TranslationMode) => {
    const requestEpoch = authStateEpoch;
    const form = new FormData();
    if (typeof file === "string") {
      form.append(
        "file",
        new Blob([file], { type: "text/html" }),
        "upload.html",
      );
    } else {
      form.append("file", file);
    }
    if (mode) form.append("mode", mode);
    const res = await fetch(base + "/stories/upload", {
      method: "POST",
      credentials: "same-origin",
      body: form,
    });
    return parseResponse<{ id: string; status: string }>(
      res,
      "/stories/upload",
      undefined,
      requestEpoch,
    );
  },
  retry: (
    id: string,
    body: {
      blockIds?: string[];
      chapterIndex?: number;
      mode?: TranslationMode;
    } = {},
  ) =>
    http<{ ok: true }>(`/stories/${pathSegment(id)}/retry`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  remove: (id: string) =>
    http<{ ok: true }>(`/stories/${pathSegment(id)}`, { method: "DELETE" }),

  getTranslationStatus: (id: string, signal?: AbortSignal) =>
    http<TranslationStatusView>(
      `/stories/${pathSegment(id)}/translation-status`,
      { signal },
      Schema.TranslationStatusView,
    ),
  resetTranslationStats: (id: string) =>
    http<{ ok: true }>(`/stories/${pathSegment(id)}/translation-status/reset`, {
      method: "POST",
    }),
  reanalyze: (id: string) =>
    http<{ ok: true }>(`/stories/${pathSegment(id)}/reanalyze`, {
      method: "POST",
    }),

  getConfig: (signal?: AbortSignal) =>
    http("/config", { signal }, ConfigResponseSchema),
  getPublicConfig: (signal?: AbortSignal) =>
    http("/config/public", { signal }, PublicConfigSchema),
  saveConfig: (body: any) =>
    http<{ ok: true }>("/config", {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  testConfig: () =>
    http<{ ok: boolean; content?: string; error?: string }>("/config/test", {
      method: "POST",
    }),

  version: (signal?: AbortSignal) =>
    http<VersionInfo>("/update/version", { signal }, Schema.VersionInfo),
  checkUpdate: () => http<VersionInfo>("/update/check", { method: "POST" }),
  applyUpdate: (body: ApplyUpdateRequest = {}) =>
    http<{ ok: boolean; message: string; version?: string }>("/update/apply", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  me: () => http<AuthMe>("/auth/me", undefined, Schema.AuthMe),
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

  listUsers: (signal?: AbortSignal) =>
    http<{ users: PublicUser[] }>("/users", { signal }),
  createUser: (body: CreateUserRequest) =>
    http<{ user: PublicUser }>("/users", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  updateUser: (id: string, body: { password?: string; role?: Role }) =>
    http<{ user: PublicUser }>(`/users/${pathSegment(id)}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),
  deleteUser: (id: string) =>
    http<{ ok: true }>(`/users/${pathSegment(id)}`, { method: "DELETE" }),
};

export function subscribeStream(
  id: string,
  onEvent: (e: StreamEvent) => void,
  onError?: (e: Event) => void,
): () => void {
  const es = new EventSource(`${base}/stories/${pathSegment(id)}/stream`);
  const types = [
    "progress",
    "phase",
    "block-done",
    "block-error",
    "chapter-done",
    "llm-call",
  ] as const;
  for (const t of types) {
    es.addEventListener(t, (raw) => {
      try {
        const parsed = Schema.StreamEvent.safeParse(
          JSON.parse((raw as MessageEvent).data),
        );
        if (!parsed.success) throw parsed.error;
        if (parsed.data.type !== t) {
          throw new Error(
            `SSE event type mismatch: listener ${t}, payload ${parsed.data.type}`,
          );
        }
        onEvent(parsed.data);
      } catch (error) {
        console.error(`Invalid SSE ${t} event for story ${id}`, error);
        onError?.(new CustomEvent("protocol-error", { detail: error }));
      }
    });
  }
  if (onError) es.onerror = onError;
  return () => es.close();
}
