export type Theme = "auto" | "light" | "dark";

const KEY = "aohub.theme";

export function getTheme(): Theme {
  const t = localStorage.getItem(KEY);
  return t === "light" || t === "dark" ? t : "auto";
}

export function setTheme(t: Theme): void {
  localStorage.setItem(KEY, t);
  document.documentElement.dataset.theme = t;
}

export function cycleTheme(): Theme {
  const cur = getTheme();
  const next: Theme = cur === "auto" ? "light" : cur === "light" ? "dark" : "auto";
  setTheme(next);
  return next;
}
