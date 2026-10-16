import type { BlockSection, ChapterView } from "@ao3hub/shared";
import { ChevronDown, Feather, RotateCcw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { ReaderView } from "@/lib/reader-settings";

export type ReaderPair = ChapterView["chapter"]["pairs"][number];

export type ChapterSections = Record<BlockSection | "body", ReaderPair[]>;

/** Splits a chapter into its body and the author notes around it. */
export function groupSections(pairs: ReaderPair[]): ChapterSections {
  const out: ChapterSections = {
    workSummary: [],
    workNotes: [],
    summary: [],
    notes: [],
    body: [],
    endnotes: [],
    workEndnotes: [],
  };
  for (const pair of pairs) out[pair.section ?? "body"].push(pair);
  return out;
}

export const SECTION_LABEL: Record<BlockSection, string> = {
  workSummary: "简介",
  workNotes: "作者的话",
  summary: "本章简介",
  notes: "章前注",
  endnotes: "章末注",
  workEndnotes: "全文后记",
};

type PairHandlers = {
  view: ReaderView;
  canRetry: boolean;
  onRetry: (blockId: string) => void;
};

/**
 * A summary reads as the work's own blurb: no box, just a hairline and the
 * text. Notes are the author talking to the reader, so they sit in a quiet,
 * collapsible aside that is easy to skip.
 */
export function AuthorNote({
  section,
  pairs,
  ...handlers
}: PairHandlers & { section: BlockSection; pairs: ReaderPair[] }) {
  if (pairs.length === 0) return null;
  const isSummary = section === "workSummary" || section === "summary";
  const body = (
    <div
      className={cn(
        "prose-reader flex flex-col gap-[0.85em]",
        isSummary ? "reader-summary" : "reader-note",
      )}
    >
      {pairs.map((pair) => (
        <Pair key={pair.id} pair={pair} {...handlers} />
      ))}
    </div>
  );

  if (isSummary) {
    return (
      <section
        aria-label={SECTION_LABEL[section]}
        className="flex flex-col gap-2 border-l-2 border-primary/45 pl-5"
      >
        <h2 className="text-xs font-medium tracking-[0.12em] text-muted-foreground">
          {SECTION_LABEL[section]}
        </h2>
        {body}
      </section>
    );
  }

  return (
    <details
      open
      className="group/note rounded-xl border border-border/70 bg-muted/45 transition-colors open:bg-muted/60"
    >
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-xl px-4 py-3 text-xs font-medium tracking-[0.12em] text-muted-foreground outline-none select-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 sm:px-5 [&::-webkit-details-marker]:hidden">
        <Feather aria-hidden className="size-3.5" />
        {SECTION_LABEL[section]}
        <ChevronDown
          aria-hidden
          className="ml-auto size-4 -rotate-90 transition-transform duration-200 group-open/note:rotate-0"
        />
      </summary>
      <div className="px-4 pb-4 sm:px-5 sm:pb-5">{body}</div>
    </details>
  );
}

export function Pair({
  pair,
  view,
  canRetry,
  onRetry,
}: PairHandlers & { pair: ReaderPair }) {
  const heading = pair.type === "h2" || pair.type === "h3";
  if (pair.type === "hr") {
    return (
      <div className="flex justify-center py-6" role="separator">
        <Separator className="w-24!" />
      </div>
    );
  }

  const translated = pair.status === "done" && pair.zh;
  // "Chinese only" still falls back to the source while a block is untranslated
  // — an empty gap reads as a rendering bug.
  const showEn = view === "en" || view === "bilingual" || !translated;
  const showZh = view !== "en" && !!translated;

  return (
    <div
      data-block-id={pair.id}
      data-status={pair.status}
      className={cn("reader-block group", heading && "mt-10 mb-4")}
    >
      {showEn && <RichHTML type={pair.type} html={pair.en} />}
      {showZh && (
        <RichHTML
          type={pair.type}
          html={pair.zh!}
          className={cn("zh-shadow", showEn ? "mt-1.5" : "zh-solo")}
        />
      )}
      {pair.status === "pending" && view !== "en" && (
        <Skeleton className="mt-2 h-[0.9em] w-3/4 rounded-full" />
      )}
      {pair.status === "error" && view !== "en" && (
        <div className="mt-2 flex flex-wrap items-center gap-2 font-sans text-sm">
          <Badge variant="destructive" title={pair.error ?? ""}>
            翻译失败
          </Badge>
          {canRetry && (
            <Button variant="ghost" size="xs" onClick={() => onRetry(pair.id)}>
              <RotateCcw data-icon="inline-start" />
              重试这一段
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

const BLOCK_ROOT_RE =
  /^\s*<(?:p|div|blockquote|pre|h[1-6]|ul|ol|li|center|figure|figcaption|table|hr)(?:\s|>|\/)/i;

type RichHTMLTag =
  | "p"
  | "h2"
  | "h3"
  | "blockquote"
  | "pre"
  | "div"
  | "ul"
  | "ol";

function legacyTagFor(type: ReaderPair["type"]): RichHTMLTag {
  switch (type) {
    case "h2":
      return "h2";
    case "h3":
      return "h3";
    case "blockquote":
      return "blockquote";
    case "pre":
      return "pre";
    case "ul":
      return "ul";
    case "ol":
      return "ol";
    default:
      return "p";
  }
}

function RichHTML({
  type,
  html,
  className,
}: {
  type: ReaderPair["type"];
  html: string;
  className?: string;
}) {
  if (BLOCK_ROOT_RE.test(html)) {
    return (
      <div
        className={cn("rich-html", className)}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  }
  const Tag = legacyTagFor(type);
  return (
    <Tag
      className={cn("rich-html", className)}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
