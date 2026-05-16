import type {
  ChapterFile,
  Meta,
  Progress,
  IndexEntry,
  StoryStatus,
} from "@ao3hub/shared";
import { Meta as MetaSchema } from "@ao3hub/shared";
import { story, upsertIndex, patchIndex, loadIndex } from "./db";
import { parseAo3Html } from "./ao3/parse";
import { fetchDownloadHtml, extractWorkId } from "./ao3/fetch";
import { makeBlankTranslated, runTranslation } from "./translate/worker";
import { enqueue, setProcessor } from "./queue";
import { emit } from "./sse";

function nowIso(): string {
  return new Date().toISOString();
}

function randomId(): string {
  return "u" + Math.random().toString(36).slice(2, 10) + Date.now().toString(36).slice(-4);
}

function indexEntryFor(meta: Meta, status: StoryStatus): IndexEntry {
  return {
    id: meta.id,
    title: meta.title,
    chineseTitle: meta.chineseTitle,
    author: meta.author,
    chapterCount: meta.chapterCount,
    wordCount: meta.wordCount,
    status,
    addedAt: nowIso(),
    updatedAt: nowIso(),
  };
}

async function persistParsed(
  html: string,
  source: { url?: string; downloadUrl?: string; workId?: string },
): Promise<{ meta: Meta; original: ChapterFile; isNew: boolean }> {
  const parsed = parseAo3Html(html);
  const id = source.workId ?? parsed.meta.workIdGuess ?? randomId();
  const url = source.url ?? parsed.meta.workUrlGuess ?? `https://archiveofourown.org/works/${id}`;
  const meta: Meta = MetaSchema.parse({
    ...parsed.meta,
    id,
    url,
    downloadUrl: source.downloadUrl,
  });

  const isNew = !(await story.exists(id));
  await story.saveSource(id, html);
  await story.saveMeta(id, meta);
  await story.saveOriginal(id, parsed.original);

  const existing = await story.loadTranslated(id);
  const translated =
    existing && existing.chapters.length === parsed.original.chapters.length
      ? existing
      : makeBlankTranslated(parsed.original);
  await story.saveTranslated(id, translated);

  const total = parsed.original.chapters.reduce(
    (acc, c) => acc + c.blocks.filter((b) => b.type !== "hr" && b.html.trim()).length,
    0,
  );
  const done = translated.chapters.reduce(
    (acc, c) => acc + c.blocks.filter((b) => b.status === "done").length,
    0,
  );
  const progress: Progress = {
    phase: "queued",
    totalBlocks: total,
    doneBlocks: done,
    startedAt: nowIso(),
    errors: [],
  };
  await story.saveProgress(id, progress);

  return { meta, original: parsed.original, isNew };
}

export async function createFromUrl(url: string): Promise<{ id: string; status: StoryStatus }> {
  const workId = extractWorkId(url);
  if (!workId) throw new Error("无法从 URL 提取 work id");

  await prepareEntry({ id: workId, title: "Fetching…", author: "", status: "fetching" });
  emit(workId, { type: "phase", phase: "fetching" });

  let html: string;
  try {
    html = await fetchDownloadHtml(workId);
  } catch (e) {
    await patchIndex(workId, { status: "error" });
    emit(workId, { type: "phase", phase: "error", message: e instanceof Error ? e.message : String(e) });
    throw e;
  }

  const { meta } = await persistParsed(html, {
    url,
    downloadUrl: `https://archiveofourown.org/works/${workId}?view_full_work=true`,
    workId,
  });
  await upsertIndex(indexEntryFor(meta, "queued"));
  enqueue({ storyId: workId, type: "translate" });
  return { id: workId, status: "queued" };
}

export async function createFromHtml(html: string): Promise<{ id: string; status: StoryStatus }> {
  const { meta } = await persistParsed(html, {});
  await upsertIndex(indexEntryFor(meta, "queued"));
  enqueue({ storyId: meta.id, type: "translate" });
  return { id: meta.id, status: "queued" };
}

async function prepareEntry(partial: {
  id: string;
  title: string;
  author: string;
  status: StoryStatus;
}): Promise<void> {
  const idx = await loadIndex();
  const existing = idx.stories.find((s) => s.id === partial.id);
  if (existing) {
    await patchIndex(partial.id, { status: partial.status });
    return;
  }
  await upsertIndex({
    id: partial.id,
    title: partial.title,
    author: partial.author,
    chapterCount: 0,
    wordCount: 0,
    status: partial.status,
    addedAt: nowIso(),
    updatedAt: nowIso(),
  });
}

export async function retryStory(
  id: string,
  opts: { blockIds?: string[]; chapterIndex?: number },
): Promise<void> {
  const translated = await story.loadTranslated(id);
  const original = await story.loadOriginal(id);
  if (!translated || !original) throw new Error("story not found");

  const idSet = opts.blockIds ? new Set(opts.blockIds) : null;
  for (let ci = 0; ci < translated.chapters.length; ci++) {
    if (typeof opts.chapterIndex === "number" && ci !== opts.chapterIndex) continue;
    const ch = translated.chapters[ci];
    for (let bi = 0; bi < ch.blocks.length; bi++) {
      const b = ch.blocks[bi];
      if (idSet && !idSet.has(b.id)) continue;
      if (!idSet && b.status !== "error") continue;
      const ob = original.chapters[ci].blocks[bi];
      if (ob.type === "hr" || !ob.html.trim()) continue;
      ch.blocks[bi] = { ...b, status: "pending", html: "", error: undefined };
    }
  }
  await story.saveTranslated(id, translated);
  await patchIndex(id, { status: "translating" });
  enqueue({ storyId: id, type: "translate" });
}

export async function deleteStory(id: string): Promise<void> {
  const idx = await loadIndex();
  idx.stories = idx.stories.filter((s) => s.id !== id);
  await (await import("./db")).saveIndex(idx);
  await story.remove(id);
}

export function initWorker(): void {
  setProcessor(async (job) => {
    try {
      if (job.type === "translate" || job.type === "retry") {
        await runTranslation(job.storyId);
      }
    } catch (e) {
      console.error(`[worker] story ${job.storyId} failed:`, e);
      const msg = e instanceof Error ? e.message : String(e);
      await patchIndex(job.storyId, { status: "error" });
      emit(job.storyId, { type: "phase", phase: "error", message: msg });
    }
  });
}

export async function resumeOnStartup(): Promise<void> {
  const idx = await loadIndex();
  for (const entry of idx.stories) {
    if (entry.status === "ready" || entry.status === "error") continue;
    const p = await story.loadProgress(entry.id);
    if (!p) continue;
    if (p.phase === "ready" || p.phase === "error") continue;
    enqueue({ storyId: entry.id, type: "translate" });
  }
}
