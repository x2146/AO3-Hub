const KEY_PREFIX = "aohub.reader.";

export type ReaderSettings = {
  font: number;
  zh: number;
  measure: number;
};

const defaults: ReaderSettings = {
  font: 17,
  zh: 0.96,
  measure: 760,
};

export function loadReaderSettings(): ReaderSettings {
  return {
    font: Number(localStorage.getItem(KEY_PREFIX + "font") ?? defaults.font),
    zh: Number(localStorage.getItem(KEY_PREFIX + "zh") ?? defaults.zh),
    measure: Number(localStorage.getItem(KEY_PREFIX + "measure") ?? defaults.measure),
  };
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
