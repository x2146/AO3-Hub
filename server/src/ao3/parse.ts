import { load, type CheerioAPI } from "cheerio";
import type { Block, BlockType, Chapter, ChapterFile, Meta } from "@ao3hub/shared";

function sha1Hex(input: string): string {
  const hasher = new Bun.CryptoHasher("sha1");
  hasher.update(input);
  return hasher.digest("hex");
}

function blockId(chapterIndex: number, html: string): string {
  return sha1Hex(`${chapterIndex}::${html}`).slice(0, 8);
}

const ALLOWED_INLINE = new Set([
  "em",
  "strong",
  "b",
  "i",
  "u",
  "s",
  "a",
  "sup",
  "sub",
  "br",
  "code",
  "span",
  "font",
]);

const BLOCK_TYPES: Record<string, BlockType> = {
  p: "p",
  h2: "h2",
  h3: "h3",
  blockquote: "blockquote",
  hr: "hr",
  pre: "pre",
};

function sanitizeInline($: CheerioAPI, element: any): string {
  $(element)
    .find("script, style")
    .remove();
  return $(element).html() ?? "";
}

function textOf($: CheerioAPI, element: any): string {
  return $(element).text().replace(/\s+/g, " ").trim();
}

function stripImmersiveCruft($: CheerioAPI): void {
  $(
    ".immersive-translate-target-wrapper, .immersive-translate-target-inner, .immersive-translate-target-translation-block-wrapper, [data-immersive-translate-translation-element-mark]",
  ).remove();
  $('font[lang="zh-CN"]').remove();
  $("[data-imt-p]").removeAttr("data-imt-p");
  $("#x2146-reader-style, #x2146-reader-script, .reader-topbar, .reader-progress, .reader-controls, header.reader-title").remove();
}

function findChapterUserstuff($: CheerioAPI, root: any): any | null {
  const candidates = [root, ...$(root).find(".userstuff").toArray()];
  let best: any = null;
  let bestScore = 0;
  for (const cand of candidates) {
    if ($(cand).find(".userstuff").length > 0) continue;
    const score = $(cand).children("p, blockquote").length;
    if (score > bestScore) {
      best = cand;
      bestScore = score;
    }
  }
  return best;
}

function parseTags($: CheerioAPI, root: any): Meta["tags"] & { stats: { words?: number; chaptersDone?: number; chaptersTotal?: number; published?: string; updated?: string }; language?: string } {
  const result = {
    fandom: [] as string[],
    relationship: [] as string[],
    character: [] as string[],
    additional: [] as string[],
    rating: undefined as string | undefined,
    warnings: [] as string[],
    categories: [] as string[],
    stats: {} as {
      words?: number;
      chaptersDone?: number;
      chaptersTotal?: number;
      published?: string;
      updated?: string;
    },
    language: undefined as string | undefined,
  };
  const dl = $(root).find("dl.tags").first();
  if (!dl.length) return result;
  const dts = dl.children("dt").toArray();
  const dds = dl.children("dd").toArray();
  for (let i = 0; i < dts.length; i++) {
    const label = textOf($, dts[i]).replace(/:?$/, "").toLowerCase();
    const dd = dds[i];
    if (!dd) continue;
    const links = $(dd).find("a").toArray().map((a) => textOf($, a)).filter(Boolean);
    const text = textOf($, dd);
    switch (true) {
      case label.startsWith("rating"):
        result.rating = links[0] ?? text;
        break;
      case label.includes("archive warning"):
      case label === "warnings":
        result.warnings = links.length ? links : [text];
        break;
      case label.startsWith("categor"):
        result.categories = links.length ? links : text.split(/,\s*/);
        break;
      case label.startsWith("fandom"):
        result.fandom = links.length ? links : [text];
        break;
      case label.startsWith("relationship"):
        result.relationship = links;
        break;
      case label.startsWith("character"):
        result.character = links;
        break;
      case label.startsWith("additional tag"):
        result.additional = links;
        break;
      case label.startsWith("language"):
        result.language = text;
        break;
      case label.startsWith("stats"):
        result.stats = parseStats(text);
        break;
    }
  }
  return result;
}

function parseStats(text: string): {
  words?: number;
  chaptersDone?: number;
  chaptersTotal?: number;
  published?: string;
  updated?: string;
} {
  const out: {
    words?: number;
    chaptersDone?: number;
    chaptersTotal?: number;
    published?: string;
    updated?: string;
  } = {};
  const words = text.match(/Words:\s*([\d,]+)/i);
  if (words) out.words = Number(words[1].replace(/,/g, ""));
  const ch = text.match(/Chapters:\s*(\d+)\/(\d+|\?)/i);
  if (ch) {
    out.chaptersDone = Number(ch[1]);
    out.chaptersTotal = ch[2] === "?" ? undefined : Number(ch[2]);
  }
  const pub = text.match(/Published:\s*(\d{4}-\d{2}-\d{2})/i);
  if (pub) out.published = pub[1];
  const upd = text.match(/Updated:\s*(\d{4}-\d{2}-\d{2})/i);
  if (upd) out.updated = upd[1];
  return out;
}

function inferLanguage(text: string | undefined): string {
  if (!text) return "en";
  const t = text.trim().toLowerCase();
  if (!t || t.startsWith("english")) return "en";
  if (t.startsWith("中文") || t.includes("chinese")) return "zh";
  return t;
}

function pickBlockType(tag: string): BlockType {
  return BLOCK_TYPES[tag] ?? "p";
}

function extractBlocks($: CheerioAPI, root: any, chapterIndex: number): Block[] {
  const blocks: Block[] = [];
  $(root)
    .children()
    .each((_, el) => {
      const tag = (el as any).tagName?.toLowerCase?.() ?? "";
      if (!tag) return;
      if (tag === "hr") {
        const html = "<hr/>";
        blocks.push({
          id: blockId(chapterIndex, `hr-${blocks.length}`),
          type: "hr",
          html,
          status: "pending",
        });
        return;
      }
      const html = sanitizeInline($, el).trim();
      if (!html) return;
      const type = pickBlockType(tag);
      blocks.push({
        id: blockId(chapterIndex, html),
        type,
        html,
        status: "pending",
      });
    });
  return blocks;
}

function ensureUniqueIds(blocks: Block[]): Block[] {
  const seen = new Set<string>();
  return blocks.map((b, i) => {
    if (!seen.has(b.id)) {
      seen.add(b.id);
      return b;
    }
    let n = 1;
    let id = `${b.id}-${n}`;
    while (seen.has(id)) {
      n++;
      id = `${b.id}-${n}`;
    }
    seen.add(id);
    return { ...b, id };
  });
}

export type ParseResult = {
  meta: Omit<Meta, "id" | "url" | "downloadUrl"> & { workIdGuess?: string; workUrlGuess?: string };
  original: ChapterFile;
};

export function parseAo3Html(html: string): ParseResult {
  const $ = load(html);
  stripImmersiveCruft($);

  const preface = $("#preface").first();
  const chaptersEl = $("#chapters").first();
  const afterword = $("#afterword").first();

  const titleFromMeta = textOf($, preface.find(".meta h1").first());
  const titleFromHtmlTitle = textOf($, $("title").first()).split(" - ")[0]?.trim();
  const title = titleFromMeta || titleFromHtmlTitle || "Untitled";

  const authorEl = preface.find(".byline a[rel=author]").first().length
    ? preface.find(".byline a[rel=author]").first()
    : preface.find(".byline a").first();
  const author = textOf($, authorEl) || textOf($, preface.find(".byline").first()).replace(/^by\s+/i, "");
  const authorUrl = authorEl.attr("href") ?? undefined;

  const userstuffs = preface.find("blockquote.userstuff");
  const summary = userstuffs.eq(0).html()?.trim() || undefined;
  const notes = userstuffs.length > 1 ? userstuffs.eq(1).html()?.trim() : undefined;
  const endnotes = afterword.find("blockquote").first().html()?.trim() || undefined;

  const tags = parseTags($, preface);

  const workUrlGuess =
    preface.find('a[href*="archiveofourown.org/works/"]').first().attr("href") ?? undefined;
  const workIdGuess = workUrlGuess?.match(/\/works\/(\d+)/)?.[1];

  const chapters: Chapter[] = [];
  const groups = chaptersEl.find("> .meta.group, > .userstuff").toArray();

  if (groups.some((el) => $(el).is(".meta.group"))) {
    let currentTitle: string | undefined;
    let idx = 0;
    for (const el of groups) {
      if ($(el).is(".meta.group")) {
        const heading = textOf($, $(el).find(".heading").first());
        currentTitle = heading || undefined;
      } else if ($(el).is(".userstuff")) {
        const blocks = ensureUniqueIds(extractBlocks($, el, idx));
        chapters.push({ index: idx, title: currentTitle, blocks });
        currentTitle = undefined;
        idx++;
      }
    }
  } else {
    const userstuff = findChapterUserstuff($, chaptersEl);
    if (userstuff) {
      const chapterTitle = textOf($, chaptersEl.find("> h2.toc-heading").first()) || title;
      const blocks = ensureUniqueIds(extractBlocks($, userstuff, 0));
      chapters.push({ index: 0, title: chapterTitle, blocks });
    }
  }

  const chapterCount = chapters.length || tags.stats.chaptersDone || 1;
  const wordCount = tags.stats.words ?? 0;

  return {
    meta: {
      title,
      author,
      authorUrl,
      summary,
      notes,
      endnotes,
      tags: {
        fandom: tags.fandom,
        relationship: tags.relationship,
        character: tags.character,
        additional: tags.additional,
        rating: tags.rating,
        warnings: tags.warnings,
        categories: tags.categories,
      },
      language: inferLanguage(tags.language),
      publishedAt: tags.stats.published,
      updatedAt: tags.stats.updated,
      wordCount,
      chapterCount,
      workIdGuess,
      workUrlGuess,
    },
    original: { chapters },
  };
}
