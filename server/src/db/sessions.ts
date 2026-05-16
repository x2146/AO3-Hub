import { z } from "zod";
import { paths } from "./paths";
import { readJson, writeJson } from "./io";

const SessionRecord = z.object({
  token: z.string(),
  userId: z.string(),
  createdAt: z.string(),
  lastUsedAt: z.string(),
  expiresAt: z.string(),
});
export type SessionRecord = z.infer<typeof SessionRecord>;

const SessionsFile = z.object({
  sessions: z.array(SessionRecord).default([]),
});
type SessionsFile = z.infer<typeof SessionsFile>;

async function load(): Promise<SessionsFile> {
  const raw = await readJson<unknown>(paths.sessions());
  if (!raw) return { sessions: [] };
  const parsed = SessionsFile.safeParse(raw);
  return parsed.success ? parsed.data : { sessions: [] };
}

async function save(file: SessionsFile): Promise<void> {
  await writeJson(paths.sessions(), file);
}

function newToken(): string {
  const buf = new Uint8Array(32);
  crypto.getRandomValues(buf);
  return Array.from(buf, (b) => b.toString(16).padStart(2, "0")).join("");
}

function isExpired(s: SessionRecord, now: number): boolean {
  return Date.parse(s.expiresAt) <= now;
}

export const sessions = {
  newToken,

  create: async (input: {
    userId: string;
    ttlMs: number;
  }): Promise<SessionRecord> => {
    const file = await load();
    const now = Date.now();
    file.sessions = file.sessions.filter((s) => !isExpired(s, now));
    const created = new Date(now).toISOString();
    const record: SessionRecord = {
      token: newToken(),
      userId: input.userId,
      createdAt: created,
      lastUsedAt: created,
      expiresAt: new Date(now + input.ttlMs).toISOString(),
    };
    file.sessions.push(record);
    await save(file);
    return record;
  },

  findValid: async (token: string): Promise<SessionRecord | null> => {
    if (!token) return null;
    const file = await load();
    const now = Date.now();
    const found = file.sessions.find((s) => s.token === token);
    if (!found || isExpired(found, now)) return null;
    return found;
  },

  touch: async (token: string, ttlMs: number): Promise<SessionRecord | null> => {
    const file = await load();
    const now = Date.now();
    file.sessions = file.sessions.filter((s) => !isExpired(s, now));
    const i = file.sessions.findIndex((s) => s.token === token);
    if (i < 0) return null;
    const next: SessionRecord = {
      ...file.sessions[i],
      lastUsedAt: new Date(now).toISOString(),
      expiresAt: new Date(now + ttlMs).toISOString(),
    };
    file.sessions[i] = next;
    await save(file);
    return next;
  },

  remove: async (token: string): Promise<void> => {
    const file = await load();
    const before = file.sessions.length;
    file.sessions = file.sessions.filter((s) => s.token !== token);
    if (file.sessions.length !== before) await save(file);
  },

  removeByUser: async (userId: string): Promise<void> => {
    const file = await load();
    const before = file.sessions.length;
    file.sessions = file.sessions.filter((s) => s.userId !== userId);
    if (file.sessions.length !== before) await save(file);
  },
};
