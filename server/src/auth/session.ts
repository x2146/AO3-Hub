import type { Context } from "hono";
import { getCookie, setCookie, deleteCookie } from "hono/cookie";
import { loadConfig, sessions, users, type UserRecord } from "../db";

export const COOKIE_NAME = "ao3hub_session";

async function sessionTtlMs(): Promise<number> {
  const cfg = await loadConfig();
  return cfg.auth.sessionTtlDays * 24 * 60 * 60 * 1000;
}

export async function startSession(c: Context, userId: string): Promise<void> {
  const ttlMs = await sessionTtlMs();
  const record = await sessions.create({ userId, ttlMs });
  writeCookie(c, record.token, ttlMs);
}

export async function endSession(c: Context): Promise<void> {
  const token = getCookie(c, COOKIE_NAME);
  if (token) await sessions.remove(token);
  deleteCookie(c, COOKIE_NAME, { path: "/" });
}

function writeCookie(c: Context, token: string, ttlMs: number): void {
  setCookie(c, COOKIE_NAME, token, {
    httpOnly: true,
    sameSite: "Lax",
    path: "/",
    maxAge: Math.floor(ttlMs / 1000),
    secure: isSecureRequest(c),
  });
}

function isSecureRequest(c: Context): boolean {
  const url = new URL(c.req.url);
  if (url.protocol === "https:") return true;
  const xfProto = c.req.header("x-forwarded-proto");
  return xfProto?.split(",")[0].trim() === "https";
}

export async function resolveUser(c: Context): Promise<UserRecord | null> {
  const token = getCookie(c, COOKIE_NAME);
  if (!token) return null;
  const session = await sessions.findValid(token);
  if (!session) {
    deleteCookie(c, COOKIE_NAME, { path: "/" });
    return null;
  }
  const user = await users.findById(session.userId);
  if (!user) {
    await sessions.remove(token);
    deleteCookie(c, COOKIE_NAME, { path: "/" });
    return null;
  }
  const ttlMs = await sessionTtlMs();
  await sessions.touch(token, ttlMs);
  writeCookie(c, token, ttlMs);
  return user;
}
