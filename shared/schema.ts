import { z } from "zod";

export const StoryStatus = z.enum([
  "queued",
  "fetching",
  "parsing",
  "translating",
  "ready",
  "error",
]);
export type StoryStatus = z.infer<typeof StoryStatus>;

export const IndexEntry = z.object({
  id: z.string(),
  title: z.string(),
  chineseTitle: z.string().optional(),
  author: z.string(),
  chapterCount: z.number().int().nonnegative(),
  wordCount: z.number().int().nonnegative(),
  status: StoryStatus,
  addedAt: z.string(),
  updatedAt: z.string(),
});
export type IndexEntry = z.infer<typeof IndexEntry>;

export const Index = z.object({
  stories: z.array(IndexEntry),
});
export type Index = z.infer<typeof Index>;

export const Meta = z.object({
  id: z.string(),
  url: z.string(),
  downloadUrl: z.string().optional(),
  title: z.string(),
  chineseTitle: z.string().optional(),
  author: z.string(),
  authorUrl: z.string().optional(),
  summary: z.string().optional(),
  notes: z.string().optional(),
  endnotes: z.string().optional(),
  tags: z.object({
    fandom: z.array(z.string()).default([]),
    relationship: z.array(z.string()).default([]),
    character: z.array(z.string()).default([]),
    additional: z.array(z.string()).default([]),
    rating: z.string().optional(),
    warnings: z.array(z.string()).default([]),
    categories: z.array(z.string()).default([]),
  }),
  language: z.string().default("en"),
  publishedAt: z.string().optional(),
  updatedAt: z.string().optional(),
  wordCount: z.number().int().nonnegative().default(0),
  chapterCount: z.number().int().nonnegative().default(1),
});
export type Meta = z.infer<typeof Meta>;

export const BlockType = z.enum(["p", "h2", "h3", "blockquote", "hr", "pre"]);
export type BlockType = z.infer<typeof BlockType>;

export const BlockStatus = z.enum(["pending", "done", "error"]);
export type BlockStatus = z.infer<typeof BlockStatus>;

export const Block = z.object({
  id: z.string(),
  type: BlockType,
  html: z.string(),
  status: BlockStatus.optional(),
  error: z.string().optional(),
});
export type Block = z.infer<typeof Block>;

export const Chapter = z.object({
  index: z.number().int().nonnegative(),
  title: z.string().optional(),
  blocks: z.array(Block),
});
export type Chapter = z.infer<typeof Chapter>;

export const ChapterFile = z.object({
  chapters: z.array(Chapter),
});
export type ChapterFile = z.infer<typeof ChapterFile>;

export const ProgressPhase = z.enum([
  "queued",
  "fetching",
  "parsing",
  "translating",
  "ready",
  "error",
]);
export type ProgressPhase = z.infer<typeof ProgressPhase>;

export const ProgressError = z.object({
  chapterIndex: z.number().int().nonnegative(),
  blockId: z.string(),
  message: z.string(),
  at: z.string(),
});
export type ProgressError = z.infer<typeof ProgressError>;

export const Progress = z.object({
  phase: ProgressPhase,
  totalBlocks: z.number().int().nonnegative().default(0),
  doneBlocks: z.number().int().nonnegative().default(0),
  currentChapter: z.number().int().nonnegative().optional(),
  startedAt: z.string(),
  finishedAt: z.string().optional(),
  message: z.string().optional(),
  errors: z.array(ProgressError).default([]),
});
export type Progress = z.infer<typeof Progress>;

export const LlmConfig = z.object({
  baseURL: z.string().default("https://api.deepseek.com/v1"),
  apiKey: z.string().default(""),
  model: z.string().default("deepseek-chat"),
  temperature: z.number().default(0.3),
  concurrency: z.number().int().positive().default(3),
  blocksPerRequest: z.number().int().positive().default(8),
});
export type LlmConfig = z.infer<typeof LlmConfig>;

export const Ao3Config = z.object({
  cookie: z.string().default(""),
  userAgent: z
    .string()
    .default(
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
    ),
});
export type Ao3Config = z.infer<typeof Ao3Config>;

export const ReaderConfig = z.object({
  defaultMeasure: z.number().int().positive().default(780),
  defaultFont: z.number().int().positive().default(17),
});
export type ReaderConfig = z.infer<typeof ReaderConfig>;

export const DEFAULT_UPDATE_MANIFEST_URL =
  "https://github.com/x2146/AO3-Hub/releases/latest/download/manifest.json";

export const UpdateConfig = z.object({
  manifestURL: z.string().default(DEFAULT_UPDATE_MANIFEST_URL),
  channel: z.string().default("stable"),
  autoCheck: z.boolean().default(false),
});
export type UpdateConfig = z.infer<typeof UpdateConfig>;

export const Config = z.object({
  llm: LlmConfig,
  ao3: Ao3Config,
  reader: ReaderConfig,
  update: UpdateConfig,
});
export type Config = z.infer<typeof Config>;

export const Manifest = z.object({
  version: z.string(),
  notes: z.string().optional(),
  publishedAt: z.string().optional(),
  assets: z.array(
    z.object({
      platform: z.string(),
      arch: z.string(),
      url: z.string(),
      sha256: z.string().optional(),
      size: z.number().int().nonnegative().optional(),
    }),
  ),
});
export type Manifest = z.infer<typeof Manifest>;

export const VersionInfo = z.object({
  current: z.string(),
  platform: z.string(),
  arch: z.string(),
  builtAt: z.string().optional(),
  latest: z
    .object({
      version: z.string(),
      notes: z.string().optional(),
      publishedAt: z.string().optional(),
      hasUpdate: z.boolean(),
      downloadUrl: z.string().optional(),
    })
    .optional(),
});
export type VersionInfo = z.infer<typeof VersionInfo>;

export const CreateStoryRequest = z.object({
  url: z.string().url(),
});
export type CreateStoryRequest = z.infer<typeof CreateStoryRequest>;

export const ChapterView = z.object({
  meta: Meta,
  progress: Progress,
  chapter: z.object({
    index: z.number().int().nonnegative(),
    titleEn: z.string().optional(),
    titleZh: z.string().optional(),
    pairs: z.array(
      z.object({
        id: z.string(),
        type: BlockType,
        en: z.string(),
        zh: z.string().optional(),
        status: BlockStatus,
        error: z.string().optional(),
      }),
    ),
  }),
  nav: z.object({
    prev: z.number().int().nonnegative().optional(),
    next: z.number().int().nonnegative().optional(),
    total: z.number().int().nonnegative(),
  }),
});
export type ChapterView = z.infer<typeof ChapterView>;

export const RetryRequest = z.object({
  blockIds: z.array(z.string()).optional(),
  chapterIndex: z.number().int().nonnegative().optional(),
});
export type RetryRequest = z.infer<typeof RetryRequest>;

export type StreamEvent =
  | { type: "progress"; doneBlocks: number; totalBlocks: number; phase: ProgressPhase }
  | { type: "block-done"; chapterIndex: number; blockId: string }
  | { type: "block-error"; chapterIndex: number; blockId: string; message: string }
  | { type: "chapter-done"; chapterIndex: number }
  | { type: "phase"; phase: ProgressPhase; message?: string };
