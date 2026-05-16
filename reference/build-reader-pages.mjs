#!/usr/bin/env node
import { readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const suffix = "-reader";
const accent = "#018eee";
const appName = "x2146-fic.reader";

const sourcePattern = /-zh-CN-dual\.html$/;
const generatedPattern = new RegExp(`${suffix}\\.html$`);

const readerStyle = `
<style id="x2146-reader-style">
  :root {
    color-scheme: light dark;
    --reader-accent: ${accent};
    --reader-font-size: 17px;
    --reader-zh-scale: 0.96;
    --reader-measure: 780px;
    --reader-bg: #f7fbfc;
    --reader-surface: rgba(255, 255, 255, 0.84);
    --reader-text: #17202a;
    --reader-muted: #65717d;
    --reader-rule: rgba(23, 32, 42, 0.12);
    --reader-soft: rgba(1, 142, 238, 0.12);
    --reader-shadow: 0 18px 44px rgba(17, 24, 39, 0.12);
  }

  :root[data-reader-theme="dark"] {
    --reader-bg: #0b0f12;
    --reader-surface: rgba(18, 24, 29, 0.86);
    --reader-text: #edf4f8;
    --reader-muted: #9aabb7;
    --reader-rule: rgba(237, 244, 248, 0.14);
    --reader-soft: rgba(1, 142, 238, 0.2);
    --reader-shadow: 0 18px 44px rgba(0, 0, 0, 0.34);
  }

  @media (prefers-color-scheme: dark) {
    :root:not([data-reader-theme="light"]) {
      --reader-bg: #0b0f12;
      --reader-surface: rgba(18, 24, 29, 0.86);
      --reader-text: #edf4f8;
      --reader-muted: #9aabb7;
      --reader-rule: rgba(237, 244, 248, 0.14);
      --reader-soft: rgba(1, 142, 238, 0.2);
      --reader-shadow: 0 18px 44px rgba(0, 0, 0, 0.34);
    }
  }

  html {
    -webkit-text-size-adjust: 100%;
    text-size-adjust: 100%;
    background: var(--reader-bg);
  }

  body {
    margin: 0;
    background:
      radial-gradient(circle at top left, var(--reader-soft), transparent 30rem),
      var(--reader-bg);
    color: var(--reader-text);
    font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  }

  a {
    color: var(--reader-accent);
    text-decoration-thickness: 0.08em;
    text-underline-offset: 0.18em;
  }

  .reader-shell {
    width: min(var(--reader-measure), calc(100vw - 32px));
    margin: 0 auto;
    padding: 92px 0 72px;
  }

  .reader-topbar {
    position: fixed;
    top: max(12px, env(safe-area-inset-top));
    left: 50%;
    z-index: 9999;
    display: flex;
    width: min(820px, calc(100vw - 24px));
    min-height: 44px;
    transform: translateX(-50%);
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px;
    border: 1px solid var(--reader-rule);
    border-radius: 999px;
    background: var(--reader-surface);
    box-shadow: var(--reader-shadow);
    -webkit-backdrop-filter: blur(18px);
    backdrop-filter: blur(18px);
  }

  .reader-progress {
    position: fixed;
    top: 0;
    left: 0;
    z-index: 10000;
    width: 100%;
    height: 3px;
    background: transparent;
    pointer-events: none;
  }

  .reader-progress span {
    display: block;
    width: calc(var(--reader-progress, 0) * 100%);
    height: 100%;
    background: var(--reader-accent);
    box-shadow: 0 0 18px rgba(1, 142, 238, 0.52);
    transition: width 90ms linear;
  }

  .reader-nav {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex: 1 1 auto;
  }

  .reader-controls {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    display: none;
    align-items: center;
    gap: 6px;
    max-width: min(100vw - 24px, 820px);
    padding: 8px;
    border: 1px solid var(--reader-rule);
    border-radius: 22px;
    background: var(--reader-surface);
    box-shadow: var(--reader-shadow);
    -webkit-backdrop-filter: blur(18px);
    backdrop-filter: blur(18px);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .reader-controls::-webkit-scrollbar {
    display: none;
  }

  :root[data-reader-settings-open="true"] .reader-controls {
    display: flex;
  }

  .reader-chip,
  .reader-button {
    min-height: 34px;
    border: 1px solid transparent;
    border-radius: 999px;
    background: transparent;
    color: var(--reader-text);
    font: 650 13px/1 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  }

  .reader-chip {
    display: inline-flex;
    align-items: center;
    padding: 0 12px;
    color: var(--reader-muted);
    text-decoration: none;
  }

  .reader-current-title {
    min-width: 0;
    flex: 1 1 auto;
    overflow: hidden;
    color: var(--reader-text);
    text-overflow: ellipsis;
    white-space: nowrap;
    font: 750 13px/1.2 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  }

  .reader-progress-text {
    flex: 0 0 auto;
    min-width: 42px;
    color: var(--reader-muted);
    text-align: right;
    font: 750 12px/1 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  }

  .reader-button {
    min-width: 36px;
    padding: 0 10px;
    cursor: pointer;
    transition: background 160ms ease, color 160ms ease, transform 160ms ease;
  }

  .reader-button:hover,
  .reader-button:focus-visible {
    background: var(--reader-soft);
    color: var(--reader-accent);
    outline: none;
  }

  .reader-button:active {
    transform: translateY(1px);
  }

  .reader-status {
    min-width: 46px;
    color: var(--reader-muted);
    text-align: center;
    font: 650 12px/1 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  }

  .reader-title {
    margin: 0 0 28px;
    padding-bottom: 20px;
    border-bottom: 1px solid var(--reader-rule);
  }

  .reader-title h1 {
    margin: 0;
    color: var(--reader-text);
    font-size: clamp(2rem, 7vw, 4.25rem);
    line-height: 0.95;
    letter-spacing: 0;
  }

  .reader-title p {
    margin: 14px 0 0;
    color: var(--reader-muted);
    font-size: 0.95rem;
  }

  #preface,
  #afterword,
  #chapters {
    box-sizing: border-box;
    width: 100%;
  }

  #preface {
    margin-bottom: 32px;
    color: var(--reader-muted);
    font-size: 0.95rem;
  }

  #preface > p.message:first-of-type,
  #preface .meta h1,
  #preface .meta h2 {
    display: none;
  }

  .meta dl.tags {
    display: grid;
    grid-template-columns: minmax(90px, 150px) 1fr;
    gap: 8px 18px;
    margin: 0;
    padding: 18px 0;
    border: 0;
    border-top: 1px solid var(--reader-rule);
    border-bottom: 1px solid var(--reader-rule);
  }

  .meta dt {
    color: var(--reader-muted);
    font-weight: 700;
  }

  .meta dd {
    margin: 0 !important;
  }

  #chapters,
  .userstuff {
    padding: 0;
    color: var(--reader-text);
    font-family: Georgia, "Times New Roman", "Noto Serif SC", "Songti SC", serif;
  }

  #chapters p,
  #afterword p,
  #chapters blockquote,
  #afterword blockquote {
    font-size: var(--reader-font-size);
    line-height: 1.78;
  }

  #chapters p,
  #afterword p {
    margin: 0 0 1.28em;
  }

  #chapters .heading,
  #chapters h2:not(.toc-heading) {
    margin: 2.8em 0 1.1em;
    color: var(--reader-text);
    font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    font-size: clamp(1.35rem, 5vw, 2rem);
    line-height: 1.15;
  }

  .toc-heading {
    display: none !important;
  }

  .immersive-translate-target-wrapper[lang="zh-CN"] {
    display: block !important;
    margin-top: 0.36em;
  }

  .immersive-translate-target-wrapper[lang="zh-CN"] .immersive-translate-target-inner {
    color: var(--reader-muted);
    font-size: calc(var(--reader-font-size) * var(--reader-zh-scale)) !important;
    line-height: 1.72;
  }

  .immersive-translate-target-translation-block-wrapper {
    margin: 0 !important;
    display: block !important;
  }

  #afterword {
    margin-top: 56px;
    padding-top: 24px;
    border-top: 1px solid var(--reader-rule);
    color: var(--reader-muted);
  }

  #afterword .meta {
    color: var(--reader-muted);
  }

  ::selection {
    background: rgba(1, 142, 238, 0.25);
  }

  @media (max-width: 640px) {
    .reader-shell {
      width: min(var(--reader-measure), calc(100vw - 28px));
      padding-top: 92px;
    }

    .reader-topbar {
      justify-content: space-between;
      border-radius: 22px;
    }

    .reader-controls {
      left: 0;
      right: 0;
      flex: 0 0 auto;
    }

    .reader-chip {
      padding: 0 10px;
    }

    .reader-title {
      margin-bottom: 22px;
    }

    .reader-title h1 {
      font-size: clamp(2.1rem, 14vw, 3.5rem);
    }

    .meta dl.tags {
      grid-template-columns: 1fr;
      gap: 4px;
    }
  }
</style>`;

const readerScript = `
<script id="x2146-reader-script">
(() => {
  const root = document.documentElement;
  const store = {
    theme: "x2146.reader.theme",
    font: "x2146.reader.font",
    zh: "x2146.reader.zh",
    measure: "x2146.reader.measure"
  };
  const state = {
    theme: localStorage.getItem(store.theme) || "auto",
    font: Number(localStorage.getItem(store.font) || 17),
    zh: Number(localStorage.getItem(store.zh) || 0.96),
    measure: Number(localStorage.getItem(store.measure) || 780)
  };

  const clamp = (value, min, max) => Math.min(max, Math.max(min, value));

  function apply() {
    root.dataset.readerTheme = state.theme === "auto" ? "" : state.theme;
    if (state.theme === "auto") root.removeAttribute("data-reader-theme");
    root.style.setProperty("--reader-font-size", state.font + "px");
    root.style.setProperty("--reader-zh-scale", state.zh.toFixed(2));
    root.style.setProperty("--reader-measure", state.measure + "px");
    const size = document.querySelector("[data-reader-size]");
    const zh = document.querySelector("[data-reader-zh]");
    const width = document.querySelector("[data-reader-width]");
    const settings = document.querySelector("[data-reader-action='settings']");
    if (size) size.textContent = state.font + "px";
    if (zh) zh.textContent = Math.round(state.zh * 100) + "%";
    if (width) width.textContent = state.measure + "px";
    if (settings) settings.setAttribute("aria-expanded", root.dataset.readerSettingsOpen === "true" ? "true" : "false");
  }

  function updateProgress() {
    const max = Math.max(1, document.documentElement.scrollHeight - window.innerHeight);
    const progress = clamp(window.scrollY / max, 0, 1);
    root.style.setProperty("--reader-progress", progress.toFixed(4));
    const progressText = document.querySelector("[data-reader-progress-text]");
    if (progressText) progressText.textContent = Math.round(progress * 100) + "%";
  }

  function save() {
    localStorage.setItem(store.theme, state.theme);
    localStorage.setItem(store.font, String(state.font));
    localStorage.setItem(store.zh, String(state.zh));
    localStorage.setItem(store.measure, String(state.measure));
    apply();
  }

  function nextTheme() {
    state.theme = state.theme === "auto" ? "light" : state.theme === "light" ? "dark" : "auto";
    save();
  }

  document.addEventListener("click", (event) => {
    const action = event.target.closest("[data-reader-action]")?.dataset.readerAction;
    if (!action) return;
    if (action === "settings") {
      root.dataset.readerSettingsOpen = root.dataset.readerSettingsOpen === "true" ? "false" : "true";
      apply();
      return;
    }
    if (action === "font-down") state.font = clamp(state.font - 1, 14, 24);
    if (action === "font-up") state.font = clamp(state.font + 1, 14, 24);
    if (action === "zh-down") state.zh = clamp(Number((state.zh - 0.02).toFixed(2)), 0.9, 1.06);
    if (action === "zh-up") state.zh = clamp(Number((state.zh + 0.02).toFixed(2)), 0.9, 1.06);
    if (action === "width-down") state.measure = clamp(state.measure - 40, 620, 980);
    if (action === "width-up") state.measure = clamp(state.measure + 40, 620, 980);
    if (action === "theme") nextTheme();
    if (action === "reset") {
      state.font = 17;
      state.zh = 0.96;
      state.measure = 780;
      state.theme = "auto";
    }
    save();
  });

  apply();
  updateProgress();
  window.addEventListener("scroll", updateProgress, { passive: true });
  window.addEventListener("resize", updateProgress);
})();
</script>`;

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function stripTags(value) {
  return value
    .replace(/<script[\s\S]*?<\/script>/gi, "")
    .replace(/<style[\s\S]*?<\/style>/gi, "")
    .replace(/<[^>]+>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function getTitle(html, file) {
  const title = html.match(/<title[^>]*>([\s\S]*?)<\/title>/i)?.[1];
  const h1 = html.match(/<h1[^>]*>([\s\S]*?)<\/h1>/i)?.[1];
  const text = stripTags(title || h1 || file);
  return text.split(" - ")[0].trim() || file.replace(/\.html$/, "");
}

function getAuthor(html) {
  const title = stripTags(html.match(/<title[^>]*>([\s\S]*?)<\/title>/i)?.[1] || "");
  const parts = title.split(" - ").map((part) => part.trim()).filter(Boolean);
  return parts[1] || "";
}

function getChineseTitle(html) {
  const h1 = html.match(/<h1[^>]*>[\s\S]*?<font[^>]+lang="zh-CN"[^>]*>[\s\S]*?<font[^>]*>([\s\S]*?)<\/font>/i)?.[1];
  return h1 ? stripTags(h1) : "";
}

function getChapterCount(html) {
  const headings = html.match(/<h2 class="heading"/g);
  return headings ? headings.length : 1;
}

function buildToolbar(meta) {
  const statusTitle = [meta.title, meta.subtitle].filter(Boolean).join(" · ");
  return `
<div class="reader-progress" aria-hidden="true"><span></span></div>
<div class="reader-topbar" aria-label="阅读设置">
  <div class="reader-nav">
    <a class="reader-chip" href="list.html">目录</a>
    <span class="reader-current-title" title="${escapeHtml(statusTitle)}">${escapeHtml(statusTitle)}</span>
  </div>
  <span class="reader-progress-text" data-reader-progress-text>0%</span>
  <button class="reader-button" type="button" data-reader-action="settings" aria-expanded="false" title="展开阅读设置">设置</button>
  <div class="reader-controls">
    <button class="reader-button" type="button" data-reader-action="theme" title="切换主题">明/暗</button>
    <button class="reader-button" type="button" data-reader-action="font-down" title="缩小正文字号">A-</button>
    <span class="reader-status" data-reader-size>17px</span>
    <button class="reader-button" type="button" data-reader-action="font-up" title="放大正文字号">A+</button>
    <button class="reader-button" type="button" data-reader-action="zh-down" title="缩小中文比例">中-</button>
    <span class="reader-status" data-reader-zh>96%</span>
    <button class="reader-button" type="button" data-reader-action="zh-up" title="放大中文比例">中+</button>
    <button class="reader-button" type="button" data-reader-action="width-down" title="收窄阅读栏">窄</button>
    <span class="reader-status" data-reader-width>780px</span>
    <button class="reader-button" type="button" data-reader-action="width-up" title="加宽阅读栏">宽</button>
    <button class="reader-button" type="button" data-reader-action="reset" title="恢复默认">重置</button>
  </div>
</div>`;
}

function addReaderChrome(html, meta) {
  let output = html;
  if (!/<meta name="viewport"/i.test(output)) {
    output = output.replace(/<meta charset="UTF-8">/i, '<meta charset="UTF-8">\n  <meta name="viewport" content="width=device-width, initial-scale=1">');
  }
  output = output.replace(/<\/head>/i, `${readerStyle}\n</head>`);
  output = output.replace(/<body([^>]*)>/i, `<body$1>\n${buildToolbar(meta)}\n<main class="reader-shell">\n<header class="reader-title"><h1>${escapeHtml(meta.title)}</h1>${meta.subtitle ? `<p>${escapeHtml(meta.subtitle)}</p>` : ""}</header>`);
  output = output.replace(/<\/body>/i, `</main>\n${readerScript}\n</body>`);
  return output;
}

function buildList(items) {
  const rows = items.map((item, index) => `
      <a class="work-row" href="${escapeHtml(item.output)}" style="--i:${index}">
        <span class="work-index">${String(index + 1).padStart(2, "0")}</span>
        <span class="work-main">
          <strong>${escapeHtml(item.title)}</strong>
          <small>${escapeHtml([item.chineseTitle, item.author].filter(Boolean).join(" · "))}</small>
        </span>
        <span class="work-meta">${item.chapterCount} ${item.chapterCount > 1 ? "chapters" : "chapter"}</span>
      </a>`).join("");

  return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${appName}</title>
  <style>
    :root {
      color-scheme: light dark;
      --accent: ${accent};
      --bg: #f8fbfc;
      --text: #13202a;
      --muted: #64727d;
      --rule: rgba(19, 32, 42, 0.12);
      --soft: rgba(1, 142, 238, 0.1);
      --surface: rgba(255, 255, 255, 0.72);
    }

    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #0b0f12;
        --text: #edf4f8;
        --muted: #98aab6;
        --rule: rgba(237, 244, 248, 0.14);
        --soft: rgba(1, 142, 238, 0.18);
        --surface: rgba(18, 24, 29, 0.74);
      }
    }

    * { box-sizing: border-box; }

    html {
      -webkit-text-size-adjust: 100%;
      text-size-adjust: 100%;
      background: var(--bg);
    }

    body {
      margin: 0;
      min-height: 100svh;
      background:
        radial-gradient(circle at 18% 0%, var(--soft), transparent 34rem),
        linear-gradient(180deg, transparent, rgba(1, 142, 238, 0.04)),
        var(--bg);
      color: var(--text);
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }

    main {
      width: min(980px, calc(100vw - 36px));
      margin: 0 auto;
      padding: 72px 0 64px;
    }

    header {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 28px;
      align-items: end;
      min-height: 32svh;
      padding-bottom: 34px;
      border-bottom: 1px solid var(--rule);
    }

    h1 {
      margin: 0;
      max-width: 760px;
      font-size: clamp(3rem, 12vw, 7.5rem);
      line-height: 0.88;
      letter-spacing: 0;
    }

    .accent {
      color: var(--accent);
    }

    .summary {
      margin: 18px 0 0;
      max-width: 560px;
      color: var(--muted);
      font-size: clamp(1rem, 2.8vw, 1.15rem);
      line-height: 1.7;
    }

    .count {
      min-width: 8rem;
      padding-bottom: 0.4rem;
      color: var(--muted);
      text-align: right;
      font-size: 0.95rem;
      line-height: 1.5;
    }

    .count strong {
      display: block;
      color: var(--accent);
      font-size: clamp(2.4rem, 6vw, 4rem);
      line-height: 1;
    }

    .works {
      margin-top: 22px;
    }

    .work-row {
      display: grid;
      grid-template-columns: 52px minmax(0, 1fr) auto;
      gap: 18px;
      align-items: center;
      min-height: 94px;
      border-bottom: 1px solid var(--rule);
      color: var(--text);
      text-decoration: none;
      animation: enter 520ms ease both;
      animation-delay: calc(var(--i) * 70ms);
      transition: background 160ms ease, color 160ms ease, transform 160ms ease;
    }

    .work-row:hover,
    .work-row:focus-visible {
      background: var(--surface);
      color: var(--accent);
      outline: none;
      transform: translateX(6px);
    }

    .work-index,
    .work-meta,
    .work-main small {
      color: var(--muted);
      font-size: 0.86rem;
    }

    .work-main strong {
      display: block;
      overflow-wrap: anywhere;
      font-size: clamp(1.45rem, 5vw, 2.35rem);
      line-height: 1.05;
      letter-spacing: 0;
    }

    .work-main small {
      display: block;
      margin-top: 8px;
      line-height: 1.4;
    }

    .work-meta {
      white-space: nowrap;
    }

    footer {
      margin-top: 28px;
      color: var(--muted);
      font-size: 0.9rem;
      line-height: 1.7;
    }

    code {
      color: var(--accent);
      font-family: "SFMono-Regular", Consolas, monospace;
    }

    @keyframes enter {
      from { opacity: 0; transform: translateY(12px); }
      to { opacity: 1; transform: translateY(0); }
    }

    @media (max-width: 680px) {
      main {
        width: min(100vw - 44px, 980px);
        padding-top: 48px;
      }

      header {
        grid-template-columns: 1fr;
        min-height: 24svh;
      }

      .count {
        text-align: left;
      }

      .work-row {
        grid-template-columns: 38px minmax(0, 1fr);
        gap: 12px;
        min-height: 88px;
        padding: 0 4px;
      }

      .work-meta {
        display: none;
      }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>x2146-fic<span class="accent">.</span>reader</h1>
        <p class="summary">双语阅读入口，已适配手机和电脑。阅读页顶部状态栏显示当前作品和滚动进度，点设置可调整字号、中文比例、栏宽和深浅色。</p>
      </div>
      <p class="count"><strong>${items.length}</strong>works</p>
    </header>
    <section class="works" aria-label="作品列表">
${rows}
    </section>
    <footer>重新生成阅读版：在当前文件夹运行 <code>node build-reader-pages.mjs</code>。</footer>
  </main>
</body>
</html>`;
}

const files = (await readdir(__dirname))
  .filter((file) => sourcePattern.test(file) && !generatedPattern.test(file))
  .sort((a, b) => a.localeCompare(b, "en"));

const items = [];

for (const file of files) {
  const html = await readFile(path.join(__dirname, file), "utf8");
  const title = getTitle(html, file);
  const chineseTitle = getChineseTitle(html);
  const author = getAuthor(html);
  const chapterCount = getChapterCount(html);
  const output = file.replace(/\.html$/, `${suffix}.html`);
  const subtitle = [chineseTitle, author].filter(Boolean).join(" · ");
  const rendered = addReaderChrome(html, { title, subtitle });
  await writeFile(path.join(__dirname, output), rendered, "utf8");
  items.push({ file, output, title, chineseTitle, author, chapterCount });
}

await writeFile(path.join(__dirname, "list.html"), buildList(items), "utf8");

console.log(`Generated ${items.length} reader pages and list.html`);
