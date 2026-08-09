export type Theme = "auto" | "light" | "dark";

const KEY = "aohub.theme";

export function getTheme(): Theme {
  try {
  const t = localStorage.getItem(KEY);
  return t === "light" || t === "dark" ? t : "auto";
  } catch {
    return "auto";
  }
}

export function setTheme(t: Theme): void {
  try {
  localStorage.setItem(KEY, t);
  } catch {
    // Storage may be unavailable in restricted browsing contexts.
  }
  document.documentElement.dataset.theme = t;
}

export function cycleTheme(): Theme {
  const cur = getTheme();
  const next: Theme =
    cur === "auto" ? "light" : cur === "light" ? "dark" : "auto";
  setTheme(next);
  return next;
}
