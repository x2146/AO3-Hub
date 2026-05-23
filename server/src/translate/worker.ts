import type {
  Block,
  ChapterFile,
  Config,
  Meta,
  Progress,
  StoryStatus,
} from "@ao3hub/shared";
import { loadConfig, patchIndex, story } from "../db";
import { emit } from "../sse";
import { chunk, isTranslatable } from "./chunker";
import { chat, LlmError } from "./provider";
import { buildUserPayload, SYSTEM_PROMPT, type TranslateInput, type TranslateOutput } from "./prompt";

const MAX_RETRIES = 2;

function nowIso(): string {
  return new Date().toISOString();
}

async function withRetry<T>(fn: () => Promise<T>, retries = MAX_RETRIES): Promise<T> {
  let lastErr: unknown;
  for (let i = 0; i <= retries; i++) {
    try {
      return await fn();
    } catch (e) {
      lastErr = e;
      if (e instanceof LlmError && (e.status === 400 || e.status === 401)) throw e;
      const backoff = 600 * Math.pow(2, i) + Math.random() * 300;
      await Bun.sleep(backoff);
    }
  }
  throw lastErr instanceof Error ? lastErr : new Error(String(lastErr));
}

function parseJsonResponse(content: string): TranslateOutput[] {
  let s = content.trim();
  if (s.startsWith("```")) s = s.replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/i, "");
  const obj = JSON.parse(s);
  const arr = (obj.blocks ?? obj.data ?? obj.results) as TranslateOutput[] | undefined;
  if (!Array.isArray(arr)) throw new Error("LLM response missing 'blocks' array");
  return arr;
}

async function translateBatch(
  cfg: Config,
  meta: Meta,
  inputs: TranslateInput[],
): Promise<TranslateOutput[]> {
  const userPayload = buildUserPayload({ title: meta.title, tags: meta.tags }, inputs);
  const result = await chat(cfg.llm, [
    { role: "system", content: SYSTEM_PROMPT },
    { role: "user", content: userPayload },
  ]);
  const out = parseJsonResponse(result.content);
  if (out.length !== inputs.length) {
    throw new Error(`段数不匹配: 输入 ${inputs.length}，输出 ${out.length}`);
  }
  const byId = new Map(out.map((o) => [String(o.id), o]));
  return inputs.map((i) => {
    const found = byId.get(i.id);
    if (!found) throw new Error(`缺少段 id=${i.id} 的译文`);
    return { id: i.id, html: String(found.html) };
  });
}

async function updateStoryStatus(storyId: string, status: StoryStatus): Promise<void> {
  await patchIndex(storyId, { status, updatedAt: nowIso() });
}

async function setProgress(storyId: string, mutator: (p: Progress) => Progress): Promise<Progress> {
  const cur = (await story.loadProgress(storyId))!;
  const next = mutator(cur);
  await story.saveProgress(storyId, next);
  return next;
}

export async function runTranslation(storyId: string): Promise<void> {
  const meta = await story.loadMeta(storyId);
  const original = await story.loadOriginal(storyId);
  let translated = await story.loadTranslated(storyId);
  if (!meta || !original) throw new Error(`missing meta/original for ${storyId}`);
  if (!translated) {
    translated = makeBlankTranslated(original);
    await story.saveTranslated(storyId, translated);
  }

  const cfg = await loadConfig();
  if (!cfg.llm.apiKey?.trim()) {
    const msg = "未配置 LLM apiKey，请到 Settings 填好后再 retry";
    await setProgress(storyId, (p) => ({ ...p, phase: "error", message: msg, finishedAt: nowIso() }));
    await updateStoryStatus(storyId, "error");
    emit(storyId, { type: "phase", phase: "error", message: msg });
    return;
  }
  await updateStoryStatus(storyId, "translating");
  await setProgress(storyId, (p) => ({ ...p, phase: "translating" }));
  emit(storyId, { type: "phase", phase: "translating" });

  for (let chIdx = 0; chIdx < original.chapters.length; chIdx++) {
    const orig = original.chapters[chIdx];
    const transCh = translated.chapters[chIdx];

    await setProgress(storyId, (p) => ({ ...p, currentChapter: chIdx }));

    const pending: Block[] = [];
    for (let bi = 0; bi < orig.blocks.length; bi++) {
      const ob = orig.blocks[bi];
      const tb = transCh.blocks[bi];
      if (!isTranslatable(ob)) {
        if (tb.status !== "done") {
          transCh.blocks[bi] = { ...tb, html: ob.html, status: "done", error: undefined };
        }
        continue;
      }
      if (tb.status === "done") continue;
      pending.push(ob);
    }
    await story.saveTranslated(storyId, translated);
    await emitProgress(storyId, translated, original);

    if (!pending.length) {
      emit(storyId, { type: "chapter-done", chapterIndex: chIdx });
      continue;
    }

    const batches = chunk(pending, cfg.llm.blocksPerRequest, cfg.llm.maxTokensPerRequest);

    const concurrency = Math.max(1, cfg.llm.concurrency);
    let cursor = 0;
    const workers = Array.from({ length: Math.min(concurrency, batches.length) }, async () => {
      while (true) {
        const i = cursor++;
        if (i >= batches.length) break;
        const batch = batches[i];
        const inputs: TranslateInput[] = batch.blocks.map((b) => ({ id: b.id, html: b.html }));

        try {
          const outs = await withRetry(() => translateBatch(cfg, meta, inputs));
          for (const o of outs) {
            const bi = transCh.blocks.findIndex((b) => b.id === o.id);
            if (bi < 0) continue;
            transCh.blocks[bi] = { ...transCh.blocks[bi], html: o.html, status: "done", error: undefined };
            emit(storyId, { type: "block-done", chapterIndex: chIdx, blockId: o.id });
          }
        } catch (e) {
          const msg = e instanceof Error ? e.message : String(e);
          for (const input of inputs) {
            const bi = transCh.blocks.findIndex((b) => b.id === input.id);
            if (bi < 0) continue;
            transCh.blocks[bi] = { ...transCh.blocks[bi], status: "error", error: msg };
            emit(storyId, { type: "block-error", chapterIndex: chIdx, blockId: input.id, message: msg });
          }
          await setProgress(storyId, (p) => ({
            ...p,
            errors: [...p.errors, ...inputs.map((b) => ({ chapterIndex: chIdx, blockId: b.id, message: msg, at: nowIso() }))],
          }));
        } finally {
          await story.saveTranslated(storyId, translated);
          await emitProgress(storyId, translated, original);
        }
      }
    });

    await Promise.all(workers);
    emit(storyId, { type: "chapter-done", chapterIndex: chIdx });
  }

  const hasErrors = translated.chapters.some((c) => c.blocks.some((b) => b.status === "error"));
  const phase = hasErrors ? "error" : "ready";
  await setProgress(storyId, (p) => ({
    ...p,
    phase,
    currentChapter: undefined,
    finishedAt: nowIso(),
  }));
  await updateStoryStatus(storyId, phase);
  emit(storyId, { type: "phase", phase });
}

export function makeBlankTranslated(original: ChapterFile): ChapterFile {
  return {
    chapters: original.chapters.map((c) => ({
      index: c.index,
      title: c.title,
      blocks: c.blocks.map((b) => ({
        id: b.id,
        type: b.type,
        html: "",
        status: isTranslatable(b) ? ("pending" as const) : ("done" as const),
      })),
    })),
  };
}

async function emitProgress(
  storyId: string,
  translated: ChapterFile,
  original: ChapterFile,
): Promise<void> {
  let total = 0;
  let done = 0;
  for (let i = 0; i < original.chapters.length; i++) {
    for (let j = 0; j < original.chapters[i].blocks.length; j++) {
      if (!isTranslatable(original.chapters[i].blocks[j])) continue;
      total++;
      if (translated.chapters[i].blocks[j].status === "done") done++;
    }
  }
  await setProgress(storyId, (p) => ({ ...p, totalBlocks: total, doneBlocks: done }));
  emit(storyId, { type: "progress", doneBlocks: done, totalBlocks: total, phase: "translating" });
}
