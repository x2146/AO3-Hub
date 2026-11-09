import { CONFIG_LIMITS } from "@ao3hub/shared";

const KEY_PREFIX = "aohub.reader.";
const POSITION_KEY = "aohub.reader.positions";
/** Reading positions are capped so localStorage can't grow without bound. */
const POSITION_LIMIT = 200;

/** Which language tracks the reader renders for each paragraph. */
export type ReaderView = "bilingual" | "zh" | "en";

/** The page colour; "auto" follows the app theme. */
export type ReaderPaper = "auto" | "paper" | "sepia" | "night";

export type ReaderTypeface = "serif" | "sans";

export type ReaderSettings = {
  font: number;
  zh: number;
  measure: number;
  leading: number;
  typeface: ReaderTypeface;
  paper: ReaderPaper;
  view: ReaderView;
  /** Show the author's notes around each chapter. */
  notes: boolean;
  /** Dim every paragraph except the one under the pointer. */
  focus: boolean;
};

export type ReaderDefaults = Pick<ReaderSettings, "font" | "zh" | "measure">;

export const DEFAULT_READER_SETTINGS: ReaderSettings = {
  font: 17,
  zh: 0.96,
  measure: 760,
  leading: 1.78,
  typeface: "serif",
  paper: "auto",
  view: "bilingual",
  notes: true,
  focus: false,
};

export const READER_LIMITS = {
  font: CONFIG_LIMITS.reader.defaultFont,
  zh: CONFIG_LIMITS.reader.defaultZhScale,
  measure: CONFIG_LIMITS.reader.defaultMeasure,
  leading: { min: 1.5, max: 2.2, step: 0.02 },
};

/**
 * Paper swatches. `background` must match the `--background` each paper sets
 * in styles.css; it also tints the browser chrome through `theme-color`.
 */
export const READER_PAPERS: {
  value: ReaderPaper;
  label: string;
  background?: string;
  ink?: string;
}[] = [
  { value: "auto", label: "跟随系统" },
  { value: "paper", label: "纸白", background: "#fbfaf7", ink: "#26241f" },
  { value: "sepia", label: "米黄", background: "#f4ecda", ink: "#3b3024" },
  { value: "night", label: "夜间", background: "#16171a", ink: "#d6d3cc" },
];

export function loadReaderSettings(
  defaults: ReaderDefaults = DEFAULT_READER_SETTINGS,
): ReaderSettings {
  return {
    font: readNumber(KEY_PREFIX + "font", defaults.font, READER_LIMITS.font),
    zh: readNumber(KEY_PREFIX + "zh", defaults.zh, READER_LIMITS.zh),
    measure: readNumber(
      KEY_PREFIX + "measure",
      defaults.measure,
      READER_LIMITS.measure,
    ),
    leading: readNumber(
      KEY_PREFIX + "leading",
      DEFAULT_READER_SETTINGS.leading,
      READER_LIMITS.leading,
    ),
    typeface: readItem(KEY_PREFIX + "typeface") === "sans" ? "sans" : "serif",
    paper: readPaper(),
    view: readView(),
    notes: readItem(KEY_PREFIX + "notes") !== "0",
    focus: readItem(KEY_PREFIX + "focus") === "1",
  };
}

function readPaper(): ReaderPaper {
  const raw = readItem(KEY_PREFIX + "paper");
  return READER_PAPERS.some((paper) => paper.value === raw)
    ? (raw as ReaderPaper)
    : DEFAULT_READER_SETTINGS.paper;
}

function readItem(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    // Storage may be unavailable in restricted browsing contexts.
    return null;
  }
}

function writeItem(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Storage may be unavailable in restricted browsing contexts.
  }
}

function readView(): ReaderView {
  const raw = readItem(KEY_PREFIX + "view");
  return raw === "zh" || raw === "en" || raw === "bilingual"
    ? raw
    : DEFAULT_READER_SETTINGS.view;
}

function readNumber(
  key: string,
  fallback: number,
  limits: { readonly min: number; readonly max: number },
): number {
  const raw = readItem(key);
  const value = raw == null || raw.trim() === "" ? fallback : Number(raw);
  if (!Number.isFinite(value)) {
    return Math.min(limits.max, Math.max(limits.min, fallback));
  }
  return Math.min(limits.max, Math.max(limits.min, value));
}

export function saveReaderSettings(s: ReaderSettings): void {
  writeItem(KEY_PREFIX + "font", String(s.font));
  writeItem(KEY_PREFIX + "zh", s.zh.toFixed(2));
  writeItem(KEY_PREFIX + "measure", String(s.measure));
  writeItem(KEY_PREFIX + "leading", s.leading.toFixed(2));
  writeItem(KEY_PREFIX + "typeface", s.typeface);
  writeItem(KEY_PREFIX + "paper", s.paper);
  writeItem(KEY_PREFIX + "view", s.view);
  writeItem(KEY_PREFIX + "notes", s.notes ? "1" : "0");
  writeItem(KEY_PREFIX + "focus", s.focus ? "1" : "0");
}

export function applyReaderSettings(s: ReaderSettings): void {
  const root = document.documentElement;
  root.style.setProperty("--reader-font-size", `${s.font}px`);
  root.style.setProperty("--reader-zh-scale", s.zh.toFixed(2));
  root.style.setProperty("--reader-measure", `${s.measure}px`);
  root.style.setProperty("--reader-line", s.leading.toFixed(2));
  root.style.setProperty(
    "--reader-font",
    s.typeface === "sans" ? "var(--font-sans)" : "var(--font-serif)",
  );
}

/* -------------------------------------------------------------------------
 * Reading positions
 *
 * Reopening a work should land where the reader left off, not at the top of
 * chapter 1. Positions are keyed by story + chapter and stored as a fraction
 * of the document height so they survive font-size and column-width changes.
 * ---------------------------------------------------------------------- */

type PositionMap = Record<string, { ratio: number; at: number }>;

const positionKey = (storyID: string, chapterIndex: number) =>
  `${storyID}:${chapterIndex}`;

function readPositions(): PositionMap {
  const raw = readItem(POSITION_KEY);
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? (parsed as PositionMap) : {};
  } catch {
    return {};
  }
}

export function saveReadingPosition(
  storyID: string,
  chapterIndex: number,
  ratio: number,
): void {
  if (!Number.isFinite(ratio)) return;
  const positions = readPositions();
  positions[positionKey(storyID, chapterIndex)] = {
    ratio: Math.min(1, Math.max(0, ratio)),
    at: Date.now(),
  };

  const entries = Object.entries(positions);
  if (entries.length > POSITION_LIMIT) {
    entries.sort((a, b) => b[1].at - a[1].at);
    writeItem(
      POSITION_KEY,
      JSON.stringify(Object.fromEntries(entries.slice(0, POSITION_LIMIT))),
    );
    return;
  }
  writeItem(POSITION_KEY, JSON.stringify(positions));
}

export function loadReadingPosition(
  storyID: string,
  chapterIndex: number,
): number {
  const entry = readPositions()[positionKey(storyID, chapterIndex)];
  return entry && Number.isFinite(entry.ratio) ? entry.ratio : 0;
}

/** The chapter of one story that was read most recently, if any. */
export function loadLastRead(
  storyID: string,
): { chapter: number; ratio: number } | null {
  const prefix = `${storyID}:`;
  let best: { chapter: number; ratio: number; at: number } | null = null;
  for (const [key, entry] of Object.entries(readPositions())) {
    if (!key.startsWith(prefix) || !Number.isFinite(entry?.ratio)) continue;
    const chapter = Number(key.slice(prefix.length));
    if (!Number.isSafeInteger(chapter)) continue;
    const at = Number.isFinite(entry.at) ? entry.at : 0;
    if (!best || at > best.at) best = { chapter, ratio: entry.ratio, at };
  }
  return best && { chapter: best.chapter, ratio: best.ratio };
}

/** Every saved position of one story, keyed by chapter index. */
export function loadReadingPositions(storyID: string): Map<number, number> {
  const out = new Map<number, number>();
  const prefix = `${storyID}:`;
  for (const [key, entry] of Object.entries(readPositions())) {
    if (!key.startsWith(prefix) || !Number.isFinite(entry?.ratio)) continue;
    const index = Number(key.slice(prefix.length));
    if (Number.isSafeInteger(index)) out.set(index, entry.ratio);
  }
  return out;
}
