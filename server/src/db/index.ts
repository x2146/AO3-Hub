import { rm, stat } from "node:fs/promises";
import {
  Config,
  Index,
  type IndexEntry,
  type Meta,
  type ChapterFile,
  type Progress,
} from "@ao3hub/shared";
import { paths, storyDir } from "./paths";
import { readJson, writeJson, readText, writeText } from "./io";

export { readJson, writeJson, readText, writeText };
export { users } from "./users";
export type { UserRecord } from "./users";
export { sessions } from "./sessions";
export type { SessionRecord } from "./sessions";

const DEFAULT_CONFIG = Config.parse({
  llm: {},
  ao3: {},
  reader: {},
  update: {},
});

export async function loadConfig() {
  const data = await readJson<unknown>(paths.config());
  if (!data) return DEFAULT_CONFIG;
  const parsed = Config.safeParse(data);
  return parsed.success ? parsed.data : DEFAULT_CONFIG;
}

export async function saveConfig(data: unknown) {
  const parsed = Config.parse(data);
  await writeJson(paths.config(), parsed);
  return parsed;
}

export async function loadIndex(): Promise<Index> {
  const data = await readJson<unknown>(paths.index());
  if (!data) return { stories: [] };
  const parsed = Index.safeParse(data);
  return parsed.success ? parsed.data : { stories: [] };
}

export async function saveIndex(idx: Index): Promise<void> {
  await writeJson(paths.index(), idx);
}

export async function upsertIndex(entry: IndexEntry): Promise<void> {
  const idx = await loadIndex();
  const i = idx.stories.findIndex((s) => s.id === entry.id);
  if (i >= 0) idx.stories[i] = entry;
  else idx.stories.unshift(entry);
  await saveIndex(idx);
}

export async function removeFromIndex(id: string): Promise<void> {
  const idx = await loadIndex();
  idx.stories = idx.stories.filter((s) => s.id !== id);
  await saveIndex(idx);
}

export async function patchIndex(
  id: string,
  patch: Partial<IndexEntry>,
): Promise<IndexEntry | null> {
  const idx = await loadIndex();
  const i = idx.stories.findIndex((s) => s.id === id);
  if (i < 0) return null;
  idx.stories[i] = { ...idx.stories[i], ...patch, updatedAt: new Date().toISOString() };
  await saveIndex(idx);
  return idx.stories[i];
}

export const story = {
  exists: async (id: string) => {
    try {
      const s = await stat(storyDir(id));
      return s.isDirectory();
    } catch {
      return false;
    }
  },
  loadMeta: (id: string) => readJson<Meta>(paths.meta(id)),
  saveMeta: (id: string, m: Meta) => writeJson(paths.meta(id), m),
  loadOriginal: (id: string) => readJson<ChapterFile>(paths.original(id)),
  saveOriginal: (id: string, c: ChapterFile) => writeJson(paths.original(id), c),
  loadTranslated: (id: string) => readJson<ChapterFile>(paths.translated(id)),
  saveTranslated: (id: string, c: ChapterFile) => writeJson(paths.translated(id), c),
  loadProgress: (id: string) => readJson<Progress>(paths.progress(id)),
  saveProgress: (id: string, p: Progress) => writeJson(paths.progress(id), p),
  saveSource: (id: string, html: string) => writeText(paths.source(id), html),
  loadSource: (id: string) => readText(paths.source(id)),
  remove: (id: string) => rm(storyDir(id), { recursive: true, force: true }),
};
