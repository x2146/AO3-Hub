import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  LibraryBig,
  MessageCircleHeart,
  RotateCcw,
  Sparkles,
} from "lucide-react";
import type { ChapterView, Progress } from "@ao3hub/shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Sheet } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { api, subscribeStream } from "../lib/api";
import { useAuth } from "../lib/auth";
import { applyTheme, getTheme } from "../lib/theme";
import {
  applyReaderSettings,
  DEFAULT_READER_SETTINGS,
  loadReaderSettings,
  loadReadingPosition,
  READER_PAPERS,
  saveReaderSettings,
  saveReadingPosition,
  type ReaderDefaults,
  type ReaderSettings,
} from "../lib/reader-settings";
import {
  TranslateProgressBar,
  TranslateProgressLegend,
  breakdownOf,
} from "../components/TranslateProgress";
import { TranslationStatusPanel } from "../components/TranslationStatusPanel";
import { StatusPill } from "../components/StatusPill";
import { ReaderDock, ReaderTopbar } from "../components/reader/ReaderChrome";
import { SettingsSheet, TocSheet } from "../components/reader/ReaderSheets";
import {
  AuthorNote,
  groupSections,
  Pair,
} from "../components/reader/ReaderText";
import { chapterHeading } from "../components/reader/chapter-title";
import { PHASE_HINT } from "../lib/status";

const AO3_WORK_URL_RE = /^https:\/\/archiveofourown\.org\/works\/\d+$/;

export function Reader() {
  const { id, chapter } = useParams({ from: "/r/$id/$chapter" });
  const parsedChapterIndex = Number(chapter);
  const chapterIndex =
    /^\d+$/.test(chapter) && Number.isSafeInteger(parsedChapterIndex)
      ? parsedChapterIndex
      : null;
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { user } = useAuth();

  const [settings, setSettings] = useState<ReaderSettings>(() =>
    loadReaderSettings(),
  );
  const [settingsInitialized, setSettingsInitialized] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [tocOpen, setTocOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [scrollProgress, setScrollProgress] = useState(0);
  const [chromeVisible, setChromeVisible] = useState(true);
  const [scrolled, setScrolled] = useState(false);
  const [titleAway, setTitleAway] = useState(false);
  const [liveProgress, setLiveProgress] = useState<Progress | null>(null);
  const settingsTriggerRef = useRef<HTMLButtonElement>(null);
  const tocTriggerRef = useRef<HTMLButtonElement>(null);
  const statusTriggerRef = useRef<HTMLButtonElement>(null);
  const headerRef = useRef<HTMLElement>(null);
  const restoredForRef = useRef<string | null>(null);

  const { data: config, isFetched: configFetched } = useQuery({
    queryKey: ["config", "public"],
    queryFn: ({ signal }) => api.getPublicConfig(signal),
  });

  const serverDefaults: ReaderDefaults = useMemo(
    () =>
      config
        ? {
            font: config.reader.defaultFont,
            zh: config.reader.defaultZhScale,
            measure: config.reader.defaultMeasure,
          }
        : DEFAULT_READER_SETTINGS,
    [config],
  );

  useEffect(() => {
    if (!configFetched) return;
    if (config) setSettings(loadReaderSettings(serverDefaults));
    setSettingsInitialized(true);
  }, [config, configFetched, serverDefaults]);

  useEffect(() => {
    applyReaderSettings(settings);
    if (settingsInitialized) saveReaderSettings(settings);
  }, [settings, settingsInitialized]);

  // A fixed paper recolours the whole document — including the portalled
  // sheets — and the browser chrome, for as long as the reader is open.
  useEffect(() => {
    const root = document.documentElement;
    const paper = READER_PAPERS.find((p) => p.value === settings.paper);
    if (paper?.background) root.dataset.readerPaper = paper.value;
    applyTheme(getTheme());
    const metas = Array.from(
      document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]'),
    );
    const saved = metas.map((meta) => meta.content);
    if (paper?.background) {
      for (const meta of metas) meta.content = paper.background;
    }
    return () => {
      delete root.dataset.readerPaper;
      applyTheme(getTheme());
      metas.forEach((meta, i) => (meta.content = saved[i]));
    };
  }, [settings.paper]);

  const { data, isLoading, error } = useQuery({
    queryKey: ["chapter", id, chapterIndex],
    queryFn: ({ signal }) => {
      if (chapterIndex === null) throw new Error("章节编号无效");
      return api.getChapter(id, chapterIndex, signal);
    },
    enabled: chapterIndex !== null,
  });

  // Scroll position drives the top progress bar, the auto-hiding chrome and
  // the per-chapter resume point — all off one rAF-throttled listener.
  useEffect(() => {
    if (chapterIndex === null) return;
    const key = `${id}:${chapterIndex}`;
    let frame = 0;
    let lastY = window.scrollY;
    let latestRatio = 0;
    let lastSavedAt = 0;

    // Nothing is written until the resume effect below has run for this
    // chapter, otherwise the mount-time measurement (scrollY 0) would clobber
    // the stored position before it is read back.
    const persist = (ratio: number, force = false) => {
      if (restoredForRef.current !== key) return;
      const now = Date.now();
      // localStorage writes are synchronous, so once a second is plenty.
      if (!force && now - lastSavedAt < 1000) return;
      lastSavedAt = now;
      saveReadingPosition(id, chapterIndex, ratio);
    };

    const measure = () => {
      frame = 0;
      const scrollable =
        document.documentElement.scrollHeight - window.innerHeight;
      const y = window.scrollY;
      const ratio =
        scrollable > 0 ? Math.min(1, Math.max(0, y / scrollable)) : 0;
      setScrollProgress(ratio);
      // Small jitters (and iOS rubber-banding) must not flap the chrome. At
      // the foot of the chapter it comes back, so the way on is always there.
      const nearEnd = scrollable > 0 && scrollable - y < 120;
      if (Math.abs(y - lastY) > 6 || y < 96 || nearEnd) {
        setChromeVisible(y < 96 || y < lastY || nearEnd);
        lastY = y;
      }
      setScrolled(y > 8);
      const header = headerRef.current;
      setTitleAway(
        header ? header.getBoundingClientRect().bottom < 56 : y > 200,
      );
      latestRatio = ratio;
      persist(ratio);
    };

    const onScroll = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(measure);
    };

    // A reload or closed tab never runs the cleanup below.
    const onPageHide = () => persist(latestRatio, true);

    measure();
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("pagehide", onPageHide);
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("pagehide", onPageHide);
      if (frame) window.cancelAnimationFrame(frame);
      persist(latestRatio, true);
    };
  }, [id, chapterIndex]);

  // Jump back to where reading stopped, once per chapter. Two frames in, so the
  // measurement happens after the chapter body has actually been laid out.
  useEffect(() => {
    if (!data || chapterIndex === null) return;
    const key = `${id}:${chapterIndex}`;
    if (restoredForRef.current === key) return;

    const ratio = loadReadingPosition(id, chapterIndex);
    restoredForRef.current = key;

    let inner = 0;
    const outer = window.requestAnimationFrame(() => {
      inner = window.requestAnimationFrame(() => {
        const scrollable =
          document.documentElement.scrollHeight - window.innerHeight;
        window.scrollTo({
          top: ratio > 0.01 && scrollable > 0 ? ratio * scrollable : 0,
          behavior: "auto",
        });
      });
    });
    return () => {
      window.cancelAnimationFrame(outer);
      if (inner) window.cancelAnimationFrame(inner);
    };
  }, [data, id, chapterIndex]);

  useEffect(() => {
    setLiveProgress(data?.progress ?? null);
  }, [id, data?.progress]);

  useEffect(() => {
    if (!data) return;
    if (data.progress.phase === "ready") return;
    const unsub = subscribeStream(id, (event) => {
      if (event.type === "progress") {
        setLiveProgress((cur) => ({
          phase: event.phase,
          totalBlocks: event.totalBlocks,
          doneBlocks: event.doneBlocks,
          errorBlocks: event.errorBlocks ?? 0,
          inflightBlocks: event.inflightBlocks ?? 0,
          startedAt: cur?.startedAt ?? new Date().toISOString(),
          finishedAt: cur?.finishedAt,
          message: cur?.message,
          errors: cur?.errors ?? [],
          currentChapter: cur?.currentChapter,
        }));
      } else if (
        event.type === "block-done" &&
        event.chapterIndex === chapterIndex
      ) {
        qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
      } else if (event.type === "phase") {
        setLiveProgress((cur) =>
          cur ? { ...cur, phase: event.phase, message: event.message } : cur,
        );
        qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
        qc.invalidateQueries({ queryKey: ["stories"] });
      } else if (event.type === "chapter-done") {
        qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
      }
    });
    return unsub;
  }, [id, chapterIndex, data?.progress.phase, qc]);

  const retryFailed = useMutation({
    mutationFn: (body: { blockIds?: string[]; chapterIndex?: number }) =>
      api.retry(id, body),
    onSuccess: () => {
      toast.success("已重新入队");
      qc.invalidateQueries({ queryKey: ["chapter", id] });
      qc.invalidateQueries({ queryKey: ["stories"] });
    },
    onError: (retryError) =>
      toast.error("重试失败", { description: retryError.message }),
  });

  // A plain retry re-reads the saved source when the story predates the
  // current parser, which is what picks up the author's notes.
  const reparse = useMutation({
    mutationFn: () => api.retry(id, {}),
    onSuccess: () => {
      toast.success("正在补全作者的话", {
        description: "已有译文保持不变，只翻译新增的段落。",
      });
      qc.invalidateQueries({ queryKey: ["chapter", id] });
      qc.invalidateQueries({ queryKey: ["stories"] });
    },
    onError: (reparseError) =>
      toast.error("补全失败", { description: reparseError.message }),
  });

  const goToChapter = useCallback(
    (next: number | undefined) => {
      if (next === undefined) return;
      navigate({
        to: "/r/$id/$chapter",
        params: { id, chapter: String(next) },
      });
    },
    [id, navigate],
  );

  const anyPanelOpen = settingsOpen || tocOpen || statusOpen;

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.isContentEditable ||
          ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
      ) {
        return;
      }
      // Radix owns Escape and arrow keys while a panel has focus.
      if (anyPanelOpen) return;

      switch (event.key) {
        case "ArrowLeft":
          if (data?.nav.prev !== undefined) {
            event.preventDefault();
            goToChapter(data.nav.prev);
          }
          break;
        case "ArrowRight":
          if (data?.nav.next !== undefined) {
            event.preventDefault();
            goToChapter(data.nav.next);
          }
          break;
        case "t":
        case "T":
          if ((data?.nav.total ?? 0) > 1) {
            event.preventDefault();
            setTocOpen(true);
          }
          break;
        case "s":
        case "S":
          event.preventDefault();
          setSettingsOpen(true);
          break;
        case "e":
        case "E":
          event.preventDefault();
          setSettings((cur) => ({
            ...cur,
            view: cur.view === "bilingual" ? "zh" : "bilingual",
          }));
          break;
        default:
          break;
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    anyPanelOpen,
    data?.nav.prev,
    data?.nav.next,
    data?.nav.total,
    goToChapter,
  ]);

  const sections = useMemo(
    () => groupSections(data?.chapter.pairs ?? []),
    [data?.chapter.pairs],
  );

  if (chapterIndex === null) {
    return (
      <ReaderMessage
        title="章节编号无效"
        description="请返回书架重新选择作品。"
      />
    );
  }

  if (isLoading) {
    return (
      <div
        className="mx-auto flex flex-col gap-5 pt-[calc(env(safe-area-inset-top)+6rem)]"
        style={{ width: "min(var(--reader-measure), calc(100vw - 40px))" }}
      >
        <Skeleton className="h-3 w-28 rounded-full" />
        <Skeleton className="h-12 w-3/4" />
        <Skeleton className="h-4 w-1/3 rounded-full" />
        <div className="mt-10 flex flex-col gap-4">
          {Array.from({ length: 10 }).map((_, index) => (
            <Skeleton
              key={index}
              className="h-4 rounded-full"
              style={{ width: `${72 + ((index * 13) % 28)}%` }}
            />
          ))}
        </div>
      </div>
    );
  }

  if (error || !data) {
    return (
      <ReaderMessage
        title="章节加载失败"
        description={error?.message ?? "未知错误"}
      />
    );
  }

  const workTitle = data.meta.title;
  const rawChineseTitle =
    data.meta.chineseTitle ?? data.chapter.titleZh ?? undefined;
  // Some imports carry an untranslated `titleZh` that just repeats the English
  // title; echoing it under the heading reads like a rendering bug.
  const chineseTitle =
    rawChineseTitle &&
    rawChineseTitle !== workTitle &&
    rawChineseTitle !== data.chapter.titleEn
      ? rawChineseTitle
      : undefined;
  const total = data.nav.total;
  const multiChapter = total > 1;
  const heading = chapterHeading(data.chapter.titleEn, chapterIndex);
  const progressForBar = liveProgress ?? data.progress;
  const chapterErrorPairs = data.chapter.pairs.filter(
    (p) => p.status === "error",
  );
  const showChapterRetry =
    !!user &&
    chapterErrorPairs.length > 0 &&
    data.progress.phase !== "translating";
  const pairHandlers = {
    view: settings.view,
    canRetry: !!user,
    onRetry: (blockId: string) =>
      retryFailed.mutate({ blockIds: [blockId], chapterIndex }),
  };
  const phase = progressForBar.phase;
  const showReparse =
    !!user && !!data.reparsable && (phase === "ready" || phase === "error");

  // On phones a tap on the text toggles the chrome, as in any e-reader.
  const onArticleClick = (event: React.MouseEvent) => {
    if (!window.matchMedia("(pointer: coarse)").matches) return;
    const target = event.target as HTMLElement;
    if (target.closest("a, button, summary, input, label, [role=button]")) {
      return;
    }
    if (window.getSelection()?.toString()) return;
    setChromeVisible((visible) => !visible);
  };

  return (
    <>
      <div
        className="pointer-events-none fixed inset-x-0 top-[env(safe-area-inset-top)] z-50 h-0.5"
        aria-hidden
      >
        <div
          className="h-full bg-primary/80 transition-[width] duration-150"
          style={{ width: `${scrollProgress * 100}%` }}
        />
      </div>

      <ReaderTopbar
        visible={chromeVisible || anyPanelOpen}
        scrolled={scrolled}
        showTitle={titleAway}
        title={workTitle}
        subtitle={[
          multiChapter ? `${heading.label} / ${total}` : data.meta.author,
          `${Math.round(scrollProgress * 100)}%`,
        ].join(" · ")}
        onOpenStatus={() => setStatusOpen(true)}
        statusTriggerRef={statusTriggerRef}
      />

      <ReaderDock
        visible={chromeVisible || anyPanelOpen}
        onPrev={
          data.nav.prev !== undefined
            ? () => goToChapter(data.nav.prev)
            : undefined
        }
        onNext={
          data.nav.next !== undefined
            ? () => goToChapter(data.nav.next)
            : undefined
        }
        view={settings.view}
        onViewChange={(view) => setSettings((cur) => ({ ...cur, view }))}
        showToc={multiChapter}
        tocOpen={tocOpen}
        tocTriggerRef={tocTriggerRef}
        onToggleToc={() => setTocOpen((v) => !v)}
        settingsOpen={settingsOpen}
        settingsTriggerRef={settingsTriggerRef}
        onToggleSettings={() => setSettingsOpen((v) => !v)}
      />

      <Sheet open={settingsOpen} onOpenChange={setSettingsOpen}>
        <SettingsSheet
          settings={settings}
          setSettings={setSettings}
          defaults={serverDefaults}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            settingsTriggerRef.current?.focus();
          }}
        />
      </Sheet>

      <Sheet open={tocOpen} onOpenChange={setTocOpen}>
        <TocSheet
          data={data}
          chapterIndex={chapterIndex}
          onClose={() => setTocOpen(false)}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            tocTriggerRef.current?.focus();
          }}
        />
      </Sheet>

      <TranslationStatusPanel
        storyID={id}
        title={workTitle}
        open={statusOpen}
        onClose={() => setStatusOpen(false)}
        returnFocusRef={statusTriggerRef}
      />

      <article
        onClick={onArticleClick}
        className="mx-auto flex flex-col gap-10 pt-[calc(env(safe-area-inset-top)+5.5rem)] pb-40 sm:pt-28"
        style={{ width: "min(var(--reader-measure), calc(100vw - 40px))" }}
      >
        {chapterIndex === 0 ? (
          <TitlePage
            headerRef={headerRef}
            meta={data.meta}
            chineseTitle={chineseTitle}
            total={total}
          />
        ) : (
          <ChapterOpener
            headerRef={headerRef}
            id={id}
            workTitle={workTitle}
            heading={heading}
            total={total}
          />
        )}

        {showReparse && (
          <Alert className="bg-card/60">
            <Sparkles />
            <AlertTitle>可以补上作者的话了</AlertTitle>
            <AlertDescription className="flex flex-col items-start gap-3">
              这部作品导入时还没有提取简介、前言和后记。重新解析一次就能补上，已有译文保持不变，只翻译新增的段落。
              <Button
                variant="outline"
                size="sm"
                disabled={reparse.isPending}
                onClick={() => reparse.mutate()}
              >
                <Sparkles data-icon="inline-start" />
                补全作者的话
              </Button>
            </AlertDescription>
          </Alert>
        )}

        <ChapterProgress
          progress={progressForBar}
          chapterErrorCount={chapterErrorPairs.length}
          showChapterRetry={showChapterRetry}
          onRetryChapter={() =>
            retryFailed.mutate({
              blockIds: chapterErrorPairs.map((p) => p.id),
              chapterIndex,
            })
          }
          onRetryAll={() => retryFailed.mutate({})}
          retrying={retryFailed.isPending}
          canRetryAll={!!user}
        />

        <AuthorNote
          section="workSummary"
          pairs={sections.workSummary}
          {...pairHandlers}
        />
        {settings.notes && (
          <AuthorNote
            key={`${chapterIndex}-workNotes`}
            section="workNotes"
            pairs={sections.workNotes}
            {...pairHandlers}
          />
        )}

        {chapterIndex === 0 && multiChapter && (
          <div className="flex items-center gap-3 pt-6" role="presentation">
            <span className="shrink-0 text-xs font-medium tracking-[0.14em] text-muted-foreground">
              {heading.label}
            </span>
            {heading.subtitle && (
              <span className="reader-display min-w-0 truncate text-lg">
                {heading.subtitle}
              </span>
            )}
            <span className="h-px flex-1 bg-border" />
          </div>
        )}

        <AuthorNote
          section="summary"
          pairs={sections.summary}
          {...pairHandlers}
        />
        {settings.notes && (
          <AuthorNote
            key={`${chapterIndex}-notes`}
            section="notes"
            pairs={sections.notes}
            {...pairHandlers}
          />
        )}

        <div
          className={cn(
            "prose-reader flex flex-col gap-[1.28em]",
            settings.focus && "reader-focus",
          )}
        >
          {sections.body.map((p) => (
            <Pair key={p.id} pair={p} {...pairHandlers} />
          ))}
        </div>

        {settings.notes && (
          <>
            <AuthorNote
              key={`${chapterIndex}-endnotes`}
              section="endnotes"
              pairs={sections.endnotes}
              {...pairHandlers}
            />
            <AuthorNote
              key={`${chapterIndex}-workEndnotes`}
              section="workEndnotes"
              pairs={sections.workEndnotes}
              {...pairHandlers}
            />
          </>
        )}

        <ChapterEnd data={data} id={id} />
      </article>
    </>
  );
}

function ReaderMessage({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="mx-auto flex max-w-lg flex-col gap-4 px-4 py-32">
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>{title}</AlertTitle>
        <AlertDescription>{description}</AlertDescription>
      </Alert>
      <Button variant="outline" className="self-start" asChild>
        <Link to="/">
          <ChevronLeft data-icon="inline-start" />
          返回书架
        </Link>
      </Button>
    </div>
  );
}

function MetaDot() {
  return (
    <span aria-hidden className="size-0.5 rounded-full bg-current opacity-60" />
  );
}

/** The first chapter opens on the work itself, like a book's title page. */
function TitlePage({
  headerRef,
  meta,
  chineseTitle,
  total,
}: {
  headerRef: React.Ref<HTMLElement>;
  meta: ChapterView["meta"];
  chineseTitle?: string;
  total: number;
}) {
  const { tags } = meta;
  const facts = [
    meta.wordCount > 0 && `${meta.wordCount.toLocaleString("zh-CN")} 词`,
    total > 1 && `${total} 章`,
    tags.rating,
    meta.publishedAt,
  ].filter(Boolean) as string[];
  const otherTags = [
    ...tags.warnings,
    ...tags.categories,
    ...tags.character,
    ...tags.additional,
  ];
  const [allTags, setAllTags] = useState(false);

  return (
    <header ref={headerRef} className="flex flex-col gap-5">
      {tags.fandom.length > 0 && (
        <p className="text-xs font-medium tracking-[0.08em] text-primary">
          {tags.fandom.join(" · ")}
        </p>
      )}
      <div className="flex flex-col gap-3">
        <h1 className="reader-display text-[clamp(2.1rem,6.5vw,3.4rem)] leading-[1.06] font-semibold tracking-tight text-balance break-words [overflow-wrap:anywhere]">
          {meta.title}
        </h1>
        {chineseTitle && (
          <p className="reader-display text-xl text-muted-foreground">
            {chineseTitle}
          </p>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm text-muted-foreground">
        {meta.author && (
          <span className="font-medium text-foreground">{meta.author}</span>
        )}
        {facts.map((fact) => (
          <span key={fact} className="contents">
            <MetaDot />
            <span className="tabular-nums">{fact}</span>
          </span>
        ))}
      </div>
      {(tags.relationship.length > 0 || otherTags.length > 0) && (
        <div className="flex flex-wrap items-center gap-1.5">
          {tags.relationship.map((tag) => (
            <Badge key={tag} variant="secondary" className="font-normal">
              {tag}
            </Badge>
          ))}
          {allTags
            ? otherTags.map((tag) => (
                <Badge key={tag} variant="outline" className="font-normal">
                  {tag}
                </Badge>
              ))
            : otherTags.length > 0 && (
                <Button
                  variant="ghost"
                  size="xs"
                  className="rounded-full text-muted-foreground"
                  onClick={() => setAllTags(true)}
                >
                  +{otherTags.length} 个标签
                </Button>
              )}
        </div>
      )}
    </header>
  );
}

function ChapterOpener({
  headerRef,
  id,
  workTitle,
  heading,
  total,
}: {
  headerRef: React.Ref<HTMLElement>;
  id: string;
  workTitle: string;
  heading: { label: string; subtitle?: string };
  total: number;
}) {
  return (
    <header ref={headerRef} className="flex flex-col gap-3">
      <Link
        to="/r/$id/$chapter"
        params={{ id, chapter: "0" }}
        className="self-start text-xs font-medium tracking-[0.08em] text-muted-foreground transition-colors hover:text-foreground"
      >
        {workTitle}
      </Link>
      <h1 className="reader-display text-[clamp(1.9rem,5.5vw,3rem)] leading-[1.08] font-semibold tracking-tight text-balance break-words [overflow-wrap:anywhere]">
        {heading.subtitle ?? heading.label}
      </h1>
      <p className="text-sm text-muted-foreground tabular-nums">
        {heading.subtitle ? `${heading.label} · ` : ""}共 {total} 章
      </p>
    </header>
  );
}

function ChapterEnd({ data, id }: { data: ChapterView; id: string }) {
  const { prev, next } = data.nav;
  const ao3URL = AO3_WORK_URL_RE.test(data.meta.url) ? data.meta.url : null;

  return (
    <footer className="mt-10 flex flex-col gap-5">
      {next !== undefined ? (
        <ChapterLink id={id} index={next} title={data.nav.titles[next]} />
      ) : (
        <div className="flex flex-col items-center gap-5 py-8 text-center">
          <span className="reader-display text-sm tracking-[0.4em] text-muted-foreground">
            — 全文完 —
          </span>
          <div className="flex flex-wrap justify-center gap-2">
            {ao3URL && (
              <Button variant="outline" asChild>
                <a href={ao3URL} target="_blank" rel="noopener noreferrer">
                  <MessageCircleHeart data-icon="inline-start" />去 AO3
                  给作者留言
                </a>
              </Button>
            )}
            <Button variant="ghost" asChild>
              <Link to="/">
                <LibraryBig data-icon="inline-start" />
                返回书架
              </Link>
            </Button>
          </div>
        </div>
      )}
      {prev !== undefined && (
        <Link
          to="/r/$id/$chapter"
          params={{ id, chapter: String(prev) }}
          className="inline-flex items-center gap-1 self-start rounded-md text-sm text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <ChevronLeft className="size-4" />
          上一章 · {headingText(data.nav.titles[prev], prev)}
        </Link>
      )}
    </footer>
  );
}

function headingText(title: string | undefined, index: number) {
  const heading = chapterHeading(title, index);
  return heading.subtitle ?? heading.label;
}

function ChapterLink({
  id,
  index,
  title,
}: {
  id: string;
  index: number;
  title?: string;
}) {
  const heading = chapterHeading(title, index);
  return (
    <Link
      to="/r/$id/$chapter"
      params={{ id, chapter: String(index) }}
      className="group flex items-center gap-4 rounded-2xl border bg-card px-5 py-4 text-card-foreground shadow-xs outline-none transition-[border-color,box-shadow] hover:border-primary/40 hover:shadow-md focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-xs text-muted-foreground">
          下一章{heading.subtitle ? ` · ${heading.label}` : ""}
        </span>
        <span className="reader-display truncate text-lg font-medium">
          {heading.subtitle ?? heading.label}
        </span>
      </div>
      <ChevronRight className="size-5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
    </Link>
  );
}

function ChapterProgress({
  progress,
  chapterErrorCount,
  showChapterRetry,
  onRetryChapter,
  onRetryAll,
  retrying,
  canRetryAll,
}: {
  progress: Progress | undefined | null;
  chapterErrorCount: number;
  showChapterRetry: boolean;
  onRetryChapter: () => void;
  onRetryAll: () => void;
  retrying: boolean;
  canRetryAll: boolean;
}) {
  if (!progress || progress.phase === "ready") return null;
  const { total, error } = breakdownOf(progress);
  const hasGlobalErrors = error > chapterErrorCount;
  const description = progress.message || PHASE_HINT[progress.phase];
  // A failed run can leave every block `pending` (e.g. the LLM key was never
  // configured), so the phase itself has to offer the way out.
  const failed = progress.phase === "error";
  const showRetryRow =
    canRetryAll &&
    (failed || (total > 0 && (showChapterRetry || hasGlobalErrors)));

  return (
    <Card size="sm" className="bg-card/60">
      <CardHeader>
        <CardTitle>
          <StatusPill status={progress.phase} />
        </CardTitle>
        {description && (
          <CardDescription className={cn(failed && "text-destructive")}>
            {description}
          </CardDescription>
        )}
        <CardAction>
          <TranslateProgressLegend progress={progress} />
        </CardAction>
      </CardHeader>
      <CardContent>
        <TranslateProgressBar progress={progress} />
      </CardContent>
      {showRetryRow && (
        <CardFooter className="flex-wrap gap-2">
          {showChapterRetry && (
            <Button
              variant="outline"
              size="sm"
              disabled={retrying}
              onClick={onRetryChapter}
            >
              <RotateCcw data-icon="inline-start" />
              重试本章失败 ({chapterErrorCount})
            </Button>
          )}
          {/* Single-chapter works would otherwise show the same count twice. */}
          {(hasGlobalErrors || (failed && !showChapterRetry)) && (
            <Button
              variant={showChapterRetry ? "ghost" : "outline"}
              size="sm"
              disabled={retrying}
              onClick={onRetryAll}
            >
              <RotateCcw data-icon="inline-start" />
              {failed && error === 0
                ? "重新开始翻译"
                : `重试全部失败 (${error})`}
            </Button>
          )}
        </CardFooter>
      )}
    </Card>
  );
}
