import { Hono } from "hono";
import { Config as ConfigSchema } from "@ao3hub/shared";
import { loadConfig, saveConfig, type UserRecord } from "../db";
import { chat } from "../translate/provider";
import { requireAdmin } from "../auth/middleware";

const r = new Hono<{ Variables: { user: UserRecord | null } }>();

r.use("*", requireAdmin);

function mask(key: string): string {
  if (!key) return "";
  if (key.length <= 8) return "*".repeat(key.length);
  return key.slice(0, 4) + "…" + key.slice(-4);
}

r.get("/", async (c) => {
  const cfg = await loadConfig();
  return c.json({
    ...cfg,
    llm: { ...cfg.llm, apiKey: mask(cfg.llm.apiKey), hasApiKey: !!cfg.llm.apiKey },
    ao3: { ...cfg.ao3, cookie: cfg.ao3.cookie ? "***" : "", hasCookie: !!cfg.ao3.cookie },
  });
});

r.put("/", async (c) => {
  const body = await c.req.json().catch(() => ({}));
  const current = await loadConfig();

  const merged = {
    llm: {
      ...current.llm,
      ...body.llm,
      apiKey:
        body.llm?.apiKey && !String(body.llm.apiKey).includes("…")
          ? body.llm.apiKey
          : current.llm.apiKey,
    },
    ao3: {
      ...current.ao3,
      ...body.ao3,
      cookie: body.ao3?.cookie === "***" ? current.ao3.cookie : body.ao3?.cookie ?? current.ao3.cookie,
    },
    reader: { ...current.reader, ...body.reader },
    update: { ...current.update, ...body.update },
  };
  const parsed = ConfigSchema.safeParse(merged);
  if (!parsed.success) return c.json({ error: "invalid config", details: parsed.error.flatten() }, 400);
  await saveConfig(parsed.data);
  return c.json({ ok: true });
});

r.post("/test", async (c) => {
  const cfg = await loadConfig();
  try {
    const r1 = await chat(
      cfg.llm,
      [
        { role: "system", content: "Echo the user input as JSON {\"ok\":true}." },
        { role: "user", content: "ping" },
      ],
      { jsonMode: true },
    );
    return c.json({ ok: true, content: r1.content.slice(0, 200), usage: r1.usage });
  } catch (e) {
    return c.json({ ok: false, error: e instanceof Error ? e.message : String(e) }, 200);
  }
});

export default r;
