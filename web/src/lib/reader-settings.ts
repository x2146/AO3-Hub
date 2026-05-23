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

export function loadReaderSettings(defaults = DEFAULT_READER_SETTINGS): ReaderSettings {
  return {
    font: readNumber(KEY_PREFIX + "font", defaults.font),
    zh: readNumber(KEY_PREFIX + "zh", defaults.zh),
    measure: readNumber(KEY_PREFIX + "measure", defaults.measure),
  };
}

function readNumber(key: string, fallback: number): number {
  const raw = localStorage.getItem(key);
  return raw == null ? fallback : Number(raw);
}

export function saveReaderSettings(s: ReaderSettings): void {
  localStorage.setItem(KEY_PREFIX + "font", String(s.font));
  localStorage.setItem(KEY_PREFIX + "zh", String(s.zh));
  localStorage.setItem(KEY_PREFIX + "measure", String(s.measure));
}

export function applyReaderSettings(s: ReaderSettings): void {
  const root = document.documentElement;
  root.style.setProperty("--reader-font-size", `${s.font}px`);
  root.style.setProperty("--reader-zh-scale", s.zh.toFixed(2));
  root.style.setProperty("--reader-measure", `${s.measure}px`);
}

export const READER_LIMITS = {
  font: { min: 14, max: 24, step: 1 },
  zh: { min: 0.84, max: 1.1, step: 0.02 },
  measure: { min: 600, max: 980, step: 40 },
};
