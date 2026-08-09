import { CONFIG_LIMITS } from "@ao3hub/shared";

const KEY_PREFIX = "aohub.reader.";

export type ReaderSettings = {
  font: number;
  zh: number;
  measure: number;
};

export const DEFAULT_READER_SETTINGS: ReaderSettings = {
  font: 17,
  zh: 0.96,
  measure: 760,
};

export function loadReaderSettings(
  defaults = DEFAULT_READER_SETTINGS,
): ReaderSettings {
  return {
    font: readNumber(KEY_PREFIX + "font", defaults.font, READER_LIMITS.font),
    zh: readNumber(KEY_PREFIX + "zh", defaults.zh, READER_LIMITS.zh),
    measure: readNumber(
      KEY_PREFIX + "measure",
      defaults.measure,
      READER_LIMITS.measure,
    ),
  };
}

function readNumber(
  key: string,
  fallback: number,
  limits: { readonly min: number; readonly max: number },
): number {
  let raw: string | null;
  try {
    raw = localStorage.getItem(key);
  } catch {
    return Math.min(limits.max, Math.max(limits.min, fallback));
  }
  const value = raw == null || raw.trim() === "" ? fallback : Number(raw);
  if (!Number.isFinite(value)) {
    return Math.min(limits.max, Math.max(limits.min, fallback));
  }
  return Math.min(limits.max, Math.max(limits.min, value));
}

export function saveReaderSettings(s: ReaderSettings): void {
  try {
    localStorage.setItem(KEY_PREFIX + "font", String(s.font));
    localStorage.setItem(KEY_PREFIX + "zh", String(s.zh));
    localStorage.setItem(KEY_PREFIX + "measure", String(s.measure));
  } catch {
    // Storage may be unavailable in restricted browsing contexts.
  }
}

export function applyReaderSettings(s: ReaderSettings): void {
  const root = document.documentElement;
  root.style.setProperty("--reader-font-size", `${s.font}px`);
  root.style.setProperty("--reader-zh-scale", s.zh.toFixed(2));
  root.style.setProperty("--reader-measure", `${s.measure}px`);
}

export const READER_LIMITS = {
  font: CONFIG_LIMITS.reader.defaultFont,
  zh: CONFIG_LIMITS.reader.defaultZhScale,
  measure: CONFIG_LIMITS.reader.defaultMeasure,
};
