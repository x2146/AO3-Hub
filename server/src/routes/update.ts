import { Hono } from "hono";
import { applyUpdate, scheduleExit, versionInfo } from "../update";
import { requireAdmin } from "../auth/middleware";
import type { UserRecord } from "../db";

const r = new Hono<{ Variables: { user: UserRecord | null } }>();

r.get("/version", async (c) => {
  return c.json(await versionInfo());
});

r.post("/check", requireAdmin, async (c) => {
  return c.json(await versionInfo());
});

r.post("/apply", requireAdmin, async (c) => {
  const body = await c.req.json().catch(() => ({}));
  const result = await applyUpdate({ force: !!body.force });
  if (result.ok && result.restart) {
    scheduleExit(800);
  }
  return c.json(result, result.ok ? 200 : 400);
});

export default r;
