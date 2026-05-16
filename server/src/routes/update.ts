import { Hono } from "hono";
import { applyUpdate, scheduleExit, versionInfo } from "../update";

const r = new Hono();

r.get("/version", async (c) => {
  return c.json(await versionInfo());
});

r.post("/check", async (c) => {
  return c.json(await versionInfo());
});

r.post("/apply", async (c) => {
  const body = await c.req.json().catch(() => ({}));
  const result = await applyUpdate({ force: !!body.force });
  if (result.ok && result.restart) {
    scheduleExit(800);
  }
  return c.json(result, result.ok ? 200 : 400);
});

export default r;
