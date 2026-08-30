import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  ChevronLeft,
  ChevronRight,
  ListOrdered,
  RotateCcw,
  Settings as SettingsIcon,
  X,
} from "lucide-react";
import type { ChapterView, Progress } from "@ao3hub/shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { api, subscribeStream } from "../lib/api";
import { useAuth } from "../lib/auth";
import {
  applyReaderSettings,
  DEFAULT_READER_SETTINGS,
  loadReaderSettings,
  READER_LIMITS,
  saveReaderSettings,
  type ReaderSettings,
} from "../lib/reader-settings";
import {
  TranslateProgressBar,
  TranslateProgressLegend,
  breakdownOf,
} from "../components/TranslateProgress";
import { TranslationStatusPanel } from "../components/TranslationStatusPanel";
import { PHASE_LABEL } from "../lib/status";

const BLOCK_ROOT_RE =
  /^\s*<(?:p|div|blockquote|pre|h[1-6]|ul|ol|li|center|figure|figcaption|table|hr)(?:\s|>|\/)/i;

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
  const [showAllEn, setShowAllEn] = useState(true);
  const [liveProgress, setLiveProgress] = useState<Progress | null>(null);
  const settingsTriggerRef = useRef<HTMLButtonElement>(null);
  const tocTriggerRef = useRef<HTMLButtonElement>(null);
  const statusTriggerRef = useRef<HTMLButtonElement>(null);
  const { data: config, isFetched: configFetched } = useQuery({
    queryKey: ["config", "public"],
    queryFn: ({ signal }) => api.getPublicConfig(signal),
  });

  useEffect(() => {
    if (!configFetched) return;
    if (config) {
      setSettings(
        loadReaderSettings({
          font: config.reader.defaultFont,
          zh: config.reader.defaultZhScale,
          measure: config.reader.defaultMeasure,
        }),
      );
    }
    setSettingsInitialized(true);
  }, [config, configFetched]);

  useEffect(() => {
    applyReaderSettings(settings);
    if (settingsInitialized) saveReaderSettings(settings);
  }, [settings, settingsInitialized]);

  useEffect(() => {
    let frame = 0;
    const updateScrollProgress = () => {
      frame = 0;
      const h = document.documentElement.scrollHeight - window.innerHeight;
      setScrollProgress(
        h > 0 ? Math.min(1, Math.max(0, window.scrollY / h)) : 0,
      );
    };
    const onScroll = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(updateScrollProgress);
    };
    updateScrollProgress();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [chapterIndex]);

  const { data, isLoading, error } = useQuery({
    queryKey: ["chapter", id, chapterIndex],
    queryFn: ({ signal }) => {
      if (chapterIndex === null) throw new Error("章节编号无效");
      return api.getChapter(id, chapterIndex, signal);
    },
    enabled: chapterIndex !== null,
  });

  useEffect(() => {
    setLiveProgress(data?.progress ?? null);
  }, [id, data?.progress]);

  const total = data?.nav.total;
  const totalDigits = useMemo(
    () => (total != null ? String(total).length : 1),
    [total],
  );

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
      qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
      qc.invalidateQueries({ queryKey: ["stories"] });
    },
  });

  useEffect(() => {
    window.scrollTo({ top: 0 });
  }, [chapterIndex]);

  if (chapterIndex === null) {
    return (
      <div className="mx-auto max-w-lg py-32">
        <Alert variant="destructive">
          <X />
          <AlertTitle>章节编号无效</AlertTitle>
          <AlertDescription>
            请返回书架重新选择作品。
            <Button variant="outline" size="sm" asChild>
              <Link to="/">返回书架</Link>
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="mx-auto flex w-[min(760px,calc(100vw-32px))] flex-col gap-6 py-32">
        <Skeleton className="h-12 w-3/4" />
        <Skeleton className="h-5 w-1/3" />
        {Array.from({ length: 8 }).map((_, index) => (
          <Skeleton key={index} className="h-5 w-full" />
        ))}
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="mx-auto max-w-lg py-32">
        <Alert variant="destructive">
          <X />
          <AlertTitle>章节加载失败</AlertTitle>
          <AlertDescription>
            {error?.message ?? "未知错误"}
            <Button variant="outline" size="sm" asChild>
              <Link to="/">返回书架</Link>
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  const titleEn = data.chapter.titleEn ?? data.meta.title;
  const chineseTitle =
    data.meta.chineseTitle ?? data.chapter.titleZh ?? undefined;
  const progressForBar = liveProgress ?? data.progress;
  const chapterErrorPairs = data.chapter.pairs.filter(
    (p) => p.status === "error",
  );
  const showChapterRetry =
    !!user &&
    chapterErrorPairs.length > 0 &&
    data.progress.phase !== "translating";

  return (
    <>
      <ReaderTopbar
        title={titleEn}
        subtitle={[chineseTitle, data.meta.author].filter(Boolean).join(" · ")}
        progress={scrollProgress}
        chapterIndex={chapterIndex}
        total={total ?? 1}
        totalDigits={totalDigits}
        onPrev={
          data.nav.prev !== undefined
            ? () =>
                navigate({
                  to: "/r/$id/$chapter",
                  params: { id, chapter: String(data.nav.prev) },
                })
            : undefined
        }
        onNext={
          data.nav.next !== undefined
            ? () =>
                navigate({
                  to: "/r/$id/$chapter",
                  params: { id, chapter: String(data.nav.next) },
                })
            : undefined
        }
        settingsOpen={settingsOpen}
        settingsTriggerRef={settingsTriggerRef}
        onToggleSettings={() => {
          setSettingsOpen((v) => !v);
          setTocOpen(false);
        }}
        tocOpen={tocOpen}
        tocTriggerRef={tocTriggerRef}
        onToggleToc={() => {
          setTocOpen((v) => !v);
          setSettingsOpen(false);
        }}
        onOpenStatus={() => setStatusOpen(true)}
        statusTriggerRef={statusTriggerRef}
      />

      <Sheet open={settingsOpen} onOpenChange={setSettingsOpen}>
        <SettingsDrawer
          settings={settings}
          setSettings={setSettings}
          defaultSettings={
            config
              ? {
                  font: config.reader.defaultFont,
                  zh: config.reader.defaultZhScale,
                  measure: config.reader.defaultMeasure,
                }
              : DEFAULT_READER_SETTINGS
          }
          showAllEn={showAllEn}
          setShowAllEn={setShowAllEn}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            settingsTriggerRef.current?.focus();
          }}
        />
      </Sheet>

      <Sheet open={tocOpen} onOpenChange={setTocOpen}>
        <TocDrawer
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
        open={statusOpen}
        onClose={() => setStatusOpen(false)}
        returnFocusRef={statusTriggerRef}
      />

      <article
        className="mx-auto pb-32 pt-[152px] sm:pt-[120px]"
        style={{ width: "min(var(--reader-measure), calc(100vw - 32px))" }}
      >
        <header className="mb-12 border-b border-border pb-8">
          <h1 className="break-words text-[clamp(2rem,6vw,3.6rem)] font-semibold leading-[0.98] tracking-tight [overflow-wrap:anywhere]">
            {titleEn}
          </h1>
          {chineseTitle && (
            <p className="text-muted-foreground mt-4 text-[16px]">
              {chineseTitle}
            </p>
          )}
          {data.chapter.titleEn && data.nav.total > 1 && (
            <p className="text-muted-foreground mt-2 text-[13px] font-mono">
              CH {chapterIndex + 1}/{data.nav.total} · {data.chapter.titleEn}
            </p>
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
          {retryFailed.isError && (
            <p role="alert" className="mt-3 text-[12px] text-destructive">
              重试失败：{retryFailed.error.message}
            </p>
          )}
        </header>

        <div className="prose-reader flex flex-col gap-[1.28em]">
          {data.chapter.pairs.map((p) => (
            <Pair
              key={p.id}
              pair={p}
              showEn={showAllEn}
              canRetry={!!user}
              onRetry={(blockId) =>
                retryFailed.mutate({ blockIds: [blockId], chapterIndex })
              }
            />
          ))}
        </div>

        <ChapterNav data={data} id={id} chapterIndex={chapterIndex} />
      </article>

      <div className="fixed top-0 left-0 right-0 z-50 h-[3px] pointer-events-none">
        <div
          className="h-full bg-primary transition-[width] duration-100"
          style={{ width: `${scrollProgress * 100}%` }}
        />
      </div>
    </>
  );
}

function ReaderTopbar(props: {
  title: string;
  subtitle?: string;
  progress: number;
  chapterIndex: number;
  total: number;
  totalDigits: number;
  onPrev?: () => void;
  onNext?: () => void;
  settingsOpen: boolean;
  settingsTriggerRef: RefObject<HTMLButtonElement>;
  onToggleSettings: () => void;
  tocOpen: boolean;
  tocTriggerRef: RefObject<HTMLButtonElement>;
  onToggleToc: () => void;
  onOpenStatus: () => void;
  statusTriggerRef: RefObject<HTMLButtonElement>;
}) {
  return (
    <div className="fixed left-1/2 top-[max(0.5rem,env(safe-area-inset-top))] z-40 grid w-[calc(100vw-16px)] max-w-[820px] -translate-x-1/2 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-2 gap-y-1 rounded-card border border-border px-2 py-1.5 shadow-float surface sm:flex sm:w-[min(820px,calc(100vw-24px))] sm:rounded-full">
      <Button variant="ghost" size="sm" asChild>
        <Link to="/">
          <ChevronLeft data-icon="inline-start" />
          <span className="hidden sm:inline">目录</span>
        </Link>
      </Button>
      <div className="min-w-0 flex-1 px-1">
        <p className="truncate text-[13px] font-semibold leading-tight">
          {props.title}
        </p>
        {props.subtitle && (
          <p className="text-muted-foreground truncate text-[11px] leading-tight">
            {props.subtitle}
          </p>
        )}
      </div>
      <div className="flex items-center gap-0.5 text-[12px] font-mono text-muted-foreground">
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={props.onPrev}
          disabled={!props.onPrev}
          aria-label="上一章"
        >
          <ChevronLeft data-icon="inline-start" />
        </Button>
        <span className="tabular-nums">
          {String(props.chapterIndex + 1).padStart(props.totalDigits, "0")}/
          {props.total}
        </span>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={props.onNext}
          disabled={!props.onNext}
          aria-label="下一章"
        >
          <ChevronRight data-icon="inline-start" />
          </Button>
      </div>
      <Separator className="col-span-3 sm:hidden" />
      <div className="col-span-3 flex min-w-0 items-center justify-end gap-1 sm:contents">
        <span className="mr-auto px-1 font-mono text-[11px] tabular-nums text-muted-foreground sm:mr-0">
          {Math.round(props.progress * 100)}%
        </span>
        {props.total > 1 && (
          <Button
            ref={props.tocTriggerRef}
            variant={props.tocOpen ? "secondary" : "ghost"}
            size="sm"
            onClick={props.onToggleToc}
            aria-label="章节目录"
          >
            <ListOrdered data-icon="inline-start" />
            <span className="hidden sm:inline">章节</span>
          </Button>
        )}
        <Button
          ref={props.statusTriggerRef}
          variant="ghost"
          size="sm"
          onClick={props.onOpenStatus}
          aria-label="翻译状态"
        >
          <Activity data-icon="inline-start" />
          <span className="hidden sm:inline">状态</span>
        </Button>
        <Button
          ref={props.settingsTriggerRef}
          variant={props.settingsOpen ? "secondary" : "ghost"}
          size="sm"
          onClick={props.onToggleSettings}
          aria-label="阅读设置"
        >
          <SettingsIcon data-icon="inline-start" />
          <span className="hidden sm:inline">设置</span>
        </Button>
      </div>
    </div>
  );
}

function SettingsDrawer({
  settings,
  setSettings,
  defaultSettings,
  showAllEn,
  setShowAllEn,
  onCloseAutoFocus,
}: {
  settings: ReaderSettings;
  setSettings: (s: ReaderSettings) => void;
  defaultSettings: ReaderSettings;
  showAllEn: boolean;
  setShowAllEn: (v: boolean) => void;
  onCloseAutoFocus: (event: Event) => void;
}) {
  return (
    <SheetContent
      side="right"
      onCloseAutoFocus={onCloseAutoFocus}
      className="w-full overflow-y-auto sm:max-w-md"
    >
      <SheetHeader>
        <SheetTitle>阅读设置</SheetTitle>
        <SheetDescription>调整字号、译文比例和正文栏宽。</SheetDescription>
      </SheetHeader>
      <div className="flex flex-col gap-5 px-4">
        <ReaderSlider
          label="字号"
          value={settings.font}
          min={READER_LIMITS.font.min}
          max={READER_LIMITS.font.max}
          step={READER_LIMITS.font.step}
          format={(v) => `${v}px`}
          onChange={(v) => setSettings({ ...settings, font: v })}
        />
        <ReaderSlider
          label="中文比例"
          value={settings.zh}
          min={READER_LIMITS.zh.min}
          max={READER_LIMITS.zh.max}
          step={READER_LIMITS.zh.step}
          format={(v) => `${Math.round(v * 100)}%`}
          onChange={(v) =>
            setSettings({ ...settings, zh: Number(v.toFixed(2)) })
          }
        />
        <ReaderSlider
          label="栏宽"
          value={settings.measure}
          min={READER_LIMITS.measure.min}
          max={READER_LIMITS.measure.max}
          step={READER_LIMITS.measure.step}
          format={(v) => `${v}px`}
          onChange={(v) => setSettings({ ...settings, measure: v })}
        />
        <Separator />
        <Field orientation="horizontal" className="justify-between">
          <FieldLabel htmlFor="reader-show-source">显示原文</FieldLabel>
          <Switch
            id="reader-show-source"
            checked={showAllEn}
            onCheckedChange={setShowAllEn}
            aria-label="显示原文"
          />
        </Field>
        <Button
          variant="outline"
          size="sm"
          className="self-start"
          onClick={() => setSettings(defaultSettings)}
        >
          <RotateCcw data-icon="inline-start" />
          恢复默认
        </Button>
      </div>
    </SheetContent>
  );
}

function ReaderSlider({
  label,
  value,
  min,
  max,
  step,
  format,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  format: (v: number) => string;
  onChange: (v: number) => void;
}) {
  return (
    <Field>
      <div className="flex items-center justify-between gap-4">
        <FieldLabel>{label}</FieldLabel>
        <span className="font-mono text-xs tabular-nums text-muted-foreground">
          {format(value)}
        </span>
      </div>
      <Slider
        aria-label={label}
        value={[value]}
        min={min}
        max={max}
        step={step}
        onValueChange={(arr) => arr[0] !== undefined && onChange(arr[0])}
      />
    </Field>
  );
}

function TocDrawer({
  data,
  chapterIndex,
  onClose,
  onCloseAutoFocus,
}: {
  data: ChapterView;
  chapterIndex: number;
  onClose: () => void;
  onCloseAutoFocus: (event: Event) => void;
}) {
  const items = Array.from({ length: data.nav.total }, (_, i) => i);
  return (
    <SheetContent
      side="left"
      onCloseAutoFocus={onCloseAutoFocus}
      className="w-full overflow-y-auto p-3 sm:max-w-md"
    >
      <SheetHeader>
        <SheetTitle>章节目录</SheetTitle>
        <SheetDescription>共 {data.nav.total} 章，当前为第 {chapterIndex + 1} 章。</SheetDescription>
      </SheetHeader>
      <ul className="flex flex-col gap-1 px-1">
        {items.map((i) => (
          <li key={i}>
            <Link
              to="/r/$id/$chapter"
              params={{ id: data.meta.id, chapter: String(i) }}
              onClick={onClose}
              className={cn(
                "flex items-center justify-between rounded-control px-3 py-2 text-[13px] transition-colors hover:bg-secondary",
                i === chapterIndex && "bg-accent text-accent-foreground",
              )}
            >
              <span className="font-mono text-muted-foreground">
                {String(i + 1).padStart(2, "0")}
              </span>
              <span className="ml-3 flex-1 truncate">Chapter {i + 1}</span>
            </Link>
          </li>
        ))}
      </ul>
    </SheetContent>
  );
}

function ChapterNav({
  data,
  id,
  chapterIndex,
}: {
  data: ChapterView;
  id: string;
  chapterIndex: number;
}) {
  return (
    <>
      <Separator className="mt-20" />
      <nav className="flex items-center justify-between pt-8 text-sm text-muted-foreground">
        {data.nav.prev !== undefined ? (
          <Button variant="ghost" asChild>
            <Link
              to="/r/$id/$chapter"
              params={{ id, chapter: String(data.nav.prev) }}
            >
              <ChevronLeft data-icon="inline-start" />
              上一章
            </Link>
          </Button>
        ) : (
          <span />
        )}
        <span className="font-mono tabular-nums">
          {chapterIndex + 1}/{data.nav.total}
        </span>
        {data.nav.next !== undefined ? (
          <Button variant="ghost" asChild>
            <Link
              to="/r/$id/$chapter"
              params={{ id, chapter: String(data.nav.next) }}
            >
              下一章
              <ChevronRight data-icon="inline-end" />
            </Link>
          </Button>
        ) : (
          <span />
        )}
      </nav>
    </>
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
  const phaseLabel = PHASE_LABEL[progress.phase];
  return (
    <div className="mt-6 flex flex-col gap-2 rounded-lg border bg-card/70 p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-muted-foreground text-[12px]">
          {phaseLabel}
          {progress.message ? ` · ${progress.message}` : null}
        </span>
        <TranslateProgressLegend progress={progress} />
      </div>
      <TranslateProgressBar progress={progress} />
      {total > 0 && (showChapterRetry || hasGlobalErrors) && canRetryAll && (
        <div className="flex flex-wrap items-center gap-2 pt-1">
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
          {hasGlobalErrors && (
            <Button
              variant="ghost"
              size="sm"
              disabled={retrying}
              onClick={onRetryAll}
            >
              <RotateCcw data-icon="inline-start" />
              重试全部失败 ({error})
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

function Pair({
  pair,
  showEn,
  canRetry,
  onRetry,
}: {
  pair: ChapterView["chapter"]["pairs"][number];
  showEn: boolean;
  canRetry: boolean;
  onRetry: (blockId: string) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const heading = pair.type === "h2" || pair.type === "h3";
  if (pair.type === "hr") return <Separator className="my-8" />;

  return (
    <div
      ref={ref}
      data-block-id={pair.id}
      data-status={pair.status}
      className={cn("group", heading && "mt-12 mb-6")}
    >
      {showEn && <RichHTML type={pair.type} html={pair.en} />}
      {pair.status === "done" && pair.zh && (
        <RichHTML
          type={pair.type}
          html={pair.zh}
          className={cn("zh-shadow", showEn && "mt-1.5")}
        />
      )}
      {pair.status === "pending" && (
        <Skeleton className="mt-1.5 h-6 w-3/4" />
      )}
      {pair.status === "error" && (
        <div className="mt-1.5 flex items-center gap-2 text-sm text-destructive [&_svg]:size-4">
          <X />
          <span title={pair.error ?? ""}>翻译失败</span>
          {canRetry && (
            <Button
              variant="ghost"
              size="xs"
              onClick={() => onRetry(pair.id)}
            >
              <RotateCcw data-icon="inline-start" />
              重试
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

type RichHTMLTag =
  | "p"
  | "h2"
  | "h3"
  | "blockquote"
  | "pre"
  | "div"
  | "ul"
  | "ol";

function legacyTagFor(
  type: ChapterView["chapter"]["pairs"][number]["type"],
): RichHTMLTag {
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
  type: ChapterView["chapter"]["pairs"][number]["type"];
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
