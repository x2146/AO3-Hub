import type { Context, MiddlewareHandler } from "hono";
import type { UserRecord } from "../db";
import { resolveUser } from "./session";

type Vars = { user: UserRecord | null };

export const attachUser: MiddlewareHandler<{ Variables: Vars }> = async (c, next) => {
  const user = await resolveUser(c);
  c.set("user", user);
  await next();
};

export const requireAuth: MiddlewareHandler<{ Variables: Vars }> = async (c, next) => {
  const user = c.get("user");
  if (!user) return c.json({ error: "未登录" }, 401);
  await next();
};

export const requireAdmin: MiddlewareHandler<{ Variables: Vars }> = async (c, next) => {
  const user = c.get("user");
  if (!user) return c.json({ error: "未登录" }, 401);
  if (user.role !== "admin") return c.json({ error: "需要管理员权限" }, 403);
  await next();
};

export function currentUser(c: Context): UserRecord | null {
  return (c.get("user") as UserRecord | null) ?? null;
}
