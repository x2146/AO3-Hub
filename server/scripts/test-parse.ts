import { parseAo3Html } from "../src/ao3/parse";

const file = process.argv[2] ?? "../reference/Broken_Boy-zh-CN-dual-reader.html";
const html = await Bun.file(file).text();
const out = parseAo3Html(html);

console.log("meta:", JSON.stringify(
  {
    title: out.meta.title,
    author: out.meta.author,
    workIdGuess: out.meta.workIdGuess,
    chapterCount: out.meta.chapterCount,
    wordCount: out.meta.wordCount,
    language: out.meta.language,
    rating: out.meta.tags.rating,
    fandom: out.meta.tags.fandom,
    summaryLength: out.meta.summary?.length ?? 0,
  },
  null,
  2,
));

for (const c of out.original.chapters) {
  console.log(`chapter[${c.index}] ${c.title ?? ""} blocks=${c.blocks.length}`);
  const sample = c.blocks.slice(0, 3).map((b) => ({
    id: b.id,
    type: b.type,
    htmlPreview: b.html.replace(/<[^>]+>/g, "").slice(0, 80),
  }));
  console.log(sample);
}
