import { Hono } from "hono";
import {
  ChapterView as ChapterViewSchema,
  CreateStoryRequest,
  RetryRequest,
  type ChapterView,
} from "@ao3hub/shared";
import { loadConfig, loadIndex, story, type UserRecord } from "../db";
import { requireAuth } from "../auth/middleware";
import {
  createFromHtml,
  createFromUrl,
  deleteStory,
  retryStory,
} from "../service";

const r = new Hono<{ Variables: { user: UserRecord | null } }>();

r.get("/", async (c) => {
  const idx = await loadIndex();
  return c.json(idx);
});

r.post("/", requireAuth, async (c) => {
  const body = await c.req.json().catch(() => ({}));
  const parsed = CreateStoryRequest.safeParse(body);
  if (!parsed.success) return c.json({ error: "invalid url", details: parsed.error.flatten() }, 400);
  try {
    const out = await createFromUrl(parsed.data.url);
    return c.json(out, 201);
  } catch (e) {
    return c.json({ error: e instanceof Error ? e.message : String(e) }, 500);
  }
});

r.post("/upload", requireAuth, async (c) => {
  let html: string | null = null;
  const ct = c.req.header("content-type") ?? "";
  if (ct.startsWith("multipart/form-data")) {
    const form = await c.req.formData();
    const file = form.get("file") ?? form.get("html");
    if (file instanceof File) {
      html = await file.text();
    } else if (typeof file === "string") {
      html = file;
    }
  } else if (ct.includes("text/html") || ct.startsWith("text/plain")) {
    html = await c.req.text();
  } else {
    html = await c.req.text();
  }
  const cfg = await loadConfig();
  if (!html || html.length < cfg.import.minHtmlLength) {
    return c.json({ error: "empty or invalid html" }, 400);
  }
  try {
    const out = await createFromHtml(html);
    return c.json(out, 201);
  } catch (e) {
    return c.json({ error: e instanceof Error ? e.message : String(e) }, 500);
  }
});

r.get("/:id", async (c) => {
  const id = c.req.param("id");
  const meta = await story.loadMeta(id);
  if (!meta) return c.json({ error: "not found" }, 404);
  const progress = await story.loadProgress(id);
  return c.json({ meta, progress });
});

r.get("/:id/chapters/:n", async (c) => {
  const id = c.req.param("id");
  const n = Number(c.req.param("n"));
  if (!Number.isInteger(n) || n < 0) return c.json({ error: "invalid chapter index" }, 400);
  const meta = await story.loadMeta(id);
  const original = await story.loadOriginal(id);
  const translated = await story.loadTranslated(id);
  const progress = await story.loadProgress(id);
  if (!meta || !original || !translated || !progress) {
    return c.json({ error: "not found" }, 404);
  }
  if (n >= original.chapters.length) return c.json({ error: "chapter out of range" }, 404);

  const oCh = original.chapters[n];
  const tCh = translated.chapters[n];
  const view: ChapterView = {
    meta,
    progress,
    chapter: {
      index: n,
      titleEn: oCh.title,
      titleZh: tCh.title,
      pairs: oCh.blocks.map((b, i) => ({
        id: b.id,
        type: b.type,
        en: b.html,
        zh: tCh.blocks[i]?.html || undefined,
        status: tCh.blocks[i]?.status ?? "pending",
        error: tCh.blocks[i]?.error,
      })),
    },
    nav: {
      prev: n > 0 ? n - 1 : undefined,
      next: n + 1 < original.chapters.length ? n + 1 : undefined,
      total: original.chapters.length,
    },
  };
  const safe = ChapterViewSchema.safeParse(view);
  return c.json(safe.success ? safe.data : view);
});

r.post("/:id/retry", requireAuth, async (c) => {
  const id = c.req.param("id");
  const body = await c.req.json().catch(() => ({}));
  const parsed = RetryRequest.safeParse(body);
  if (!parsed.success) return c.json({ error: "invalid retry payload" }, 400);
  try {
    await retryStory(id, parsed.data);
    return c.json({ ok: true });
  } catch (e) {
    return c.json({ error: e instanceof Error ? e.message : String(e) }, 500);
  }
});

r.delete("/:id", requireAuth, async (c) => {
  const id = c.req.param("id");
  await deleteStory(id);
  return c.json({ ok: true });
});

export default r;
