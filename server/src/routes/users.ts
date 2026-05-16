import { Hono } from "hono";
import {
  CreateUserRequest,
  UpdateUserRequest,
} from "@ao3hub/shared";
import { sessions, users, type UserRecord } from "../db";
import { hashPassword } from "../auth/password";
import { currentUser, requireAdmin } from "../auth/middleware";

const r = new Hono<{ Variables: { user: UserRecord | null } }>();

r.use("*", requireAdmin);

r.get("/", async (c) => {
  return c.json({ users: await users.listPublic() });
});

r.post("/", async (c) => {
  const body = await c.req.json().catch(() => ({}));
  const parsed = CreateUserRequest.safeParse(body);
  if (!parsed.success) {
    return c.json({ error: "参数无效", details: parsed.error.flatten() }, 400);
  }
  try {
    const record = await users.create({
      username: parsed.data.username,
      passwordHash: await hashPassword(parsed.data.password),
      role: parsed.data.role,
    });
    return c.json({ user: users.toPublic(record) }, 201);
  } catch (e) {
    return c.json({ error: e instanceof Error ? e.message : String(e) }, 400);
  }
});

r.put("/:id", async (c) => {
  const id = c.req.param("id");
  const target = await users.findById(id);
  if (!target) return c.json({ error: "用户不存在" }, 404);

  const body = await c.req.json().catch(() => ({}));
  const parsed = UpdateUserRequest.safeParse(body);
  if (!parsed.success) {
    return c.json({ error: "参数无效", details: parsed.error.flatten() }, 400);
  }
  if (parsed.data.role && parsed.data.role !== target.role) {
    if (target.role === "admin" && (await users.adminCount()) <= 1) {
      return c.json({ error: "至少保留一个 admin" }, 400);
    }
  }
  const next = await users.update(id, {
    passwordHash: parsed.data.password
      ? await hashPassword(parsed.data.password)
      : undefined,
    role: parsed.data.role,
  });
  if (parsed.data.password) {
    await sessions.removeByUser(id);
  }
  return c.json({ user: next ? users.toPublic(next) : null });
});

r.delete("/:id", async (c) => {
  const id = c.req.param("id");
  const me = currentUser(c);
  if (me && me.id === id) return c.json({ error: "不能删除自己" }, 400);
  const target = await users.findById(id);
  if (!target) return c.json({ error: "用户不存在" }, 404);
  if (target.role === "admin" && (await users.adminCount()) <= 1) {
    return c.json({ error: "至少保留一个 admin" }, 400);
  }
  await users.remove(id);
  await sessions.removeByUser(id);
  return c.json({ ok: true });
});

export default r;
