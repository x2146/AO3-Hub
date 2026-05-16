import { Hono } from "hono";
import {
  LoginRequest,
  SetupRequest,
  type AuthMe,
} from "@ao3hub/shared";
import { users, type UserRecord } from "../db";
import { hashPassword, verifyPassword } from "../auth/password";
import { endSession, startSession } from "../auth/session";
import { currentUser } from "../auth/middleware";

const r = new Hono<{ Variables: { user: UserRecord | null } }>();

r.get("/me", async (c) => {
  const user = currentUser(c);
  const needsSetup = (await users.count()) === 0;
  const body: AuthMe = {
    user: user ? users.toPublic(user) : null,
    needsSetup,
  };
  return c.json(body);
});

r.get("/setup-status", async (c) => {
  const needsSetup = (await users.count()) === 0;
  return c.json({ needsSetup });
});

r.post("/setup", async (c) => {
  if ((await users.count()) > 0) {
    return c.json({ error: "已完成初始化" }, 409);
  }
  const body = await c.req.json().catch(() => ({}));
  const parsed = SetupRequest.safeParse(body);
  if (!parsed.success) {
    return c.json({ error: "参数无效", details: parsed.error.flatten() }, 400);
  }
  const passwordHash = await hashPassword(parsed.data.password);
  const record = await users.create({
    username: parsed.data.username,
    passwordHash,
    role: "admin",
  });
  await startSession(c, record.id);
  return c.json({ user: users.toPublic(record) }, 201);
});

r.post("/login", async (c) => {
  const body = await c.req.json().catch(() => ({}));
  const parsed = LoginRequest.safeParse(body);
  if (!parsed.success) {
    return c.json({ error: "用户名或密码无效" }, 400);
  }
  const record = await users.findByUsername(parsed.data.username);
  const ok = record && (await verifyPassword(parsed.data.password, record.passwordHash));
  if (!record || !ok) {
    return c.json({ error: "用户名或密码错误" }, 401);
  }
  await startSession(c, record.id);
  return c.json({ user: users.toPublic(record) });
});

r.post("/logout", async (c) => {
  await endSession(c);
  return c.json({ ok: true });
});

export default r;
