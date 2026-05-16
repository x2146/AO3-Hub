import type { Context } from "hono";
import { getCookie, setCookie, deleteCookie } from "hono/cookie";
import { sessions, users, type UserRecord } from "../db";

export const COOKIE_NAME = "ao3hub_session";
export const SESSION_TTL_MS = 30 * 24 * 60 * 60 * 1000;

export async function startSession(c: Context, userId: string): Promise<void> {
  const record = await sessions.create({ userId, ttlMs: SESSION_TTL_MS });
  writeCookie(c, record.token);
}

export async function endSession(c: Context): Promise<void> {
  const token = getCookie(c, COOKIE_NAME);
  if (token) await sessions.remove(token);
  deleteCookie(c, COOKIE_NAME, { path: "/" });
}

function writeCookie(c: Context, token: string): void {
  setCookie(c, COOKIE_NAME, token, {
    httpOnly: true,
    sameSite: "Lax",
    path: "/",
    maxAge: Math.floor(SESSION_TTL_MS / 1000),
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
  await sessions.touch(token, SESSION_TTL_MS);
  writeCookie(c, token);
  return user;
}
