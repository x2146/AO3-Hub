import { CONFIG_LIMITS } from "@ao3hub/shared";

const KEY_PREFIX = "aohub.reader.";
const POSITION_KEY = "aohub.reader.positions";
/** Reading positions are capped so localStorage can't grow without bound. */
const POSITION_LIMIT = 200;

/** Which language tracks the reader renders for each paragraph. */
export type ReaderView = "bilingual" | "zh" | "en";

export type ReaderSettings = {
  font: number;
  zh: number;
  measure: number;
  view: ReaderView;
  /** Dim every paragraph except the one under the pointer. */
  focus: boolean;
};

export type ReaderDefaults = Pick<ReaderSettings, "font" | "zh" | "measure">;

export const DEFAULT_READER_SETTINGS: ReaderSettings = {
  font: 17,
  zh: 0.96,
  measure: 760,
  view: "bilingual",
  focus: false,
};

export const READER_LIMITS = {
  font: CONFIG_LIMITS.reader.defaultFont,
  zh: CONFIG_LIMITS.reader.defaultZhScale,
  measure: CONFIG_LIMITS.reader.defaultMeasure,
};

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
    view: readView(),
    focus: readItem(KEY_PREFIX + "focus") === "1",
  };
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
  writeItem(KEY_PREFIX + "view", s.view);
  writeItem(KEY_PREFIX + "focus", s.focus ? "1" : "0");
}

export function applyReaderSettings(s: ReaderSettings): void {
  const root = document.documentElement;
  root.style.setProperty("--reader-font-size", `${s.font}px`);
  root.style.setProperty("--reader-zh-scale", s.zh.toFixed(2));
  root.style.setProperty("--reader-measure", `${s.measure}px`);
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
