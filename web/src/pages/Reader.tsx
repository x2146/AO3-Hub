import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronLeft,
  ChevronRight,
  ListOrdered,
  RotateCcw,
  Settings as SettingsIcon,
  X,
} from "lucide-react";
import type { ChapterView, Progress } from "@ao3hub/shared";
import { Button } from "@/components/ui/button";
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

export function Reader() {
  const { id, chapter } = useParams({ from: "/r/$id/$chapter" });
  const chapterIndex = Number(chapter);
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { user } = useAuth();

  const [settings, setSettings] = useState<ReaderSettings>(() => loadReaderSettings());
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [tocOpen, setTocOpen] = useState(false);
  const [scrollProgress, setScrollProgress] = useState(0);
  const [showAllEn, setShowAllEn] = useState(true);
  const [liveProgress, setLiveProgress] = useState<Progress | null>(null);
  const { data: config } = useQuery({
    queryKey: ["config", "public"],
    queryFn: () => api.getPublicConfig(),
  });

  useEffect(() => {
    if (!config) return;
    setSettings(
      loadReaderSettings({
        font: config.reader.defaultFont,
        zh: config.reader.defaultZhScale,
        measure: config.reader.defaultMeasure,
      }),
    );
  }, [config]);

  useEffect(() => {
    applyReaderSettings(settings);
    saveReaderSettings(settings);
  }, [settings]);

  useEffect(() => {
    const onScroll = () => {
      const h = document.documentElement.scrollHeight - window.innerHeight;
      setScrollProgress(h > 0 ? Math.min(1, Math.max(0, window.scrollY / h)) : 0);
    };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, [chapterIndex]);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["chapter", id, chapterIndex],
    queryFn: () => api.getChapter(id, chapterIndex),
  });

  useEffect(() => {
    if (!data) return;
    setLiveProgress((cur) => cur ?? data.progress);
  }, [data]);

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
      } else if (event.type === "block-done" && event.chapterIndex === chapterIndex) {
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

  if (isLoading) {
    return (
      <div className="py-32 text-center">
        <p className="text-muted-foreground">载入章节…</p>
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="py-32 text-center space-y-3">
        <p className="text-destructive">
          载入失败：{(error as Error)?.message ?? "unknown"}
        </p>
        <Button variant="outline" asChild>
          <Link to="/">返回书架</Link>
        </Button>
      </div>
    );
  }

  const titleEn = data.chapter.titleEn ?? data.meta.title;
  const chineseTitle =
    data.meta.chineseTitle ?? data.chapter.titleZh ?? undefined;
  const progressForBar = liveProgress ?? data.progress;
  const chapterErrorPairs = data.chapter.pairs.filter((p) => p.status === "error");
  const showChapterRetry =
    !!user && chapterErrorPairs.length > 0 && data.progress.phase !== "translating";

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
        onToggleSettings={() => {
          setSettingsOpen((v) => !v);
          setTocOpen(false);
        }}
        tocOpen={tocOpen}
        onToggleToc={() => {
          setTocOpen((v) => !v);
          setSettingsOpen(false);
        }}
      />

      {settingsOpen && (
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
          onClose={() => setSettingsOpen(false)}
        />
      )}

      {tocOpen && (
        <TocDrawer
          data={data}
          chapterIndex={chapterIndex}
          onClose={() => setTocOpen(false)}
        />
      )}

      <article
        className="mx-auto pt-[120px] pb-32"
        style={{ width: "min(var(--reader-measure), calc(100vw - 32px))" }}
      >
        <header className="mb-12 border-b border-border pb-8">
          <h1 className="text-[clamp(2rem,6vw,3.6rem)] font-semibold leading-[0.98] tracking-tight">
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
        </header>

        <div className="prose-reader space-y-[1.28em]">
          {data.chapter.pairs.map((p) => (
            <Pair
              key={p.id}
              pair={p}
              showEn={showAllEn}
              canRetry={!!user}
              onRetry={async (blockId) => {
                await api.retry(id, { blockIds: [blockId], chapterIndex });
                refetch();
              }}
            />
          ))}
        </div>

        <ChapterNav data={data} id={id} chapterIndex={chapterIndex} />
      </article>

      <div className="fixed top-0 left-0 right-0 z-50 h-[3px] pointer-events-none">
        <div
          className="h-full bg-accent transition-[width] duration-100"
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
  onToggleSettings: () => void;
  tocOpen: boolean;
  onToggleToc: () => void;
}) {
  return (
    <div className="fixed left-1/2 top-3 z-40 flex w-[min(820px,calc(100vw-24px))] -translate-x-1/2 items-center gap-2 rounded-full border border-border surface px-2 py-1.5 shadow-[0_18px_44px_rgba(17,24,39,0.12)]">
      <Button variant="ghost" size="sm" asChild className="gap-1">
        <Link to="/">
          <ChevronLeft className="size-3.5" />
          目录
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
          size="icon"
          className="h-8 w-7"
          onClick={props.onPrev}
          disabled={!props.onPrev}
          aria-label="上一章"
        >
          <ChevronLeft className="size-3.5" />
        </Button>
        <span className="tabular-nums">
          {String(props.chapterIndex + 1).padStart(props.totalDigits, "0")}/
          {props.total}
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-7"
          onClick={props.onNext}
          disabled={!props.onNext}
          aria-label="下一章"
        >
          <ChevronRight className="size-3.5" />
        </Button>
      </div>
      <span className="text-muted-foreground tabular-nums text-[11px] font-mono px-1">
        {Math.round(props.progress * 100)}%
      </span>
      {props.total > 1 && (
        <Button
          variant="ghost"
          size="sm"
          className={cn("gap-1", props.tocOpen && "bg-secondary")}
          onClick={props.onToggleToc}
          aria-label="章节目录"
        >
          <ListOrdered className="size-3.5" />
          章节
        </Button>
      )}
      <Button
        variant="ghost"
        size="sm"
        className={cn("gap-1", props.settingsOpen && "bg-secondary")}
        onClick={props.onToggleSettings}
        aria-label="阅读设置"
      >
        <SettingsIcon className="size-3.5" />
        设置
      </Button>
    </div>
  );
}

function SettingsDrawer({
  settings,
  setSettings,
  defaultSettings,
  showAllEn,
  setShowAllEn,
  onClose,
}: {
  settings: ReaderSettings;
  setSettings: (s: ReaderSettings) => void;
  defaultSettings: ReaderSettings;
  showAllEn: boolean;
  setShowAllEn: (v: boolean) => void;
  onClose: () => void;
}) {
  return (
    <div
      role="dialog"
      aria-label="阅读设置"
      className="fixed top-[64px] left-1/2 z-40 w-[min(560px,calc(100vw-24px))] -translate-x-1/2 rounded-3xl border border-border bg-card p-5 shadow-[0_28px_60px_rgba(17,24,39,0.18)] surface"
    >
      <div className="flex items-center justify-between">
        <p className="text-[13px] font-semibold tracking-wider uppercase text-muted-foreground">
          阅读设置
        </p>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={onClose}
          aria-label="关闭"
        >
          <X className="size-3.5" />
        </Button>
      </div>
      <div className="mt-4 grid gap-4">
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
          onChange={(v) => setSettings({ ...settings, zh: Number(v.toFixed(2)) })}
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
        <label className="flex items-center justify-between text-[13px]">
          <span>显示原文</span>
          <Switch
            checked={showAllEn}
            onCheckedChange={setShowAllEn}
            aria-label="显示原文"
          />
        </label>
        <Button
          variant="outline"
          size="sm"
          className="gap-1.5 self-start"
          onClick={() => setSettings(defaultSettings)}
        >
          <RotateCcw className="size-3.5" />
          恢复默认
        </Button>
      </div>
    </div>
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
    <div className="flex items-center gap-4">
      <span className="text-muted-foreground w-[80px] text-[12px]">{label}</span>
      <Slider
        value={[value]}
        min={min}
        max={max}
        step={step}
        onValueChange={(arr) => arr[0] !== undefined && onChange(arr[0])}
        className="flex-1"
      />
      <span className="text-muted-foreground w-[64px] text-right font-mono text-[12px] tabular-nums">
        {format(value)}
      </span>
    </div>
  );
}

function TocDrawer({
  data,
  chapterIndex,
  onClose,
}: {
  data: ChapterView;
  chapterIndex: number;
  onClose: () => void;
}) {
  const items = Array.from({ length: data.nav.total }, (_, i) => i);
  return (
    <div
      role="dialog"
      aria-label="章节目录"
      className="fixed top-[64px] left-1/2 z-40 w-[min(560px,calc(100vw-24px))] -translate-x-1/2 rounded-3xl border border-border bg-card p-3 shadow-[0_28px_60px_rgba(17,24,39,0.18)] surface max-h-[60vh] overflow-y-auto"
    >
      <div className="flex items-center justify-between px-2 py-1">
        <p className="text-[13px] font-semibold tracking-wider uppercase text-muted-foreground">
          章节目录
        </p>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={onClose}
          aria-label="关闭"
        >
          <X className="size-3.5" />
        </Button>
      </div>
      <ul className="mt-2">
        {items.map((i) => (
          <li key={i}>
            <Link
              to="/r/$id/$chapter"
              params={{ id: data.meta.id, chapter: String(i) }}
              onClick={onClose}
              className={cn(
                "flex items-center justify-between rounded-xl px-3 py-2 text-[13px] transition-colors hover:bg-secondary",
                i === chapterIndex && "bg-accent/10",
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
    </div>
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
    <nav className="mt-20 flex items-center justify-between border-t border-border pt-8 text-[13px] text-muted-foreground">
      {data.nav.prev !== undefined ? (
        <Button variant="ghost" asChild className="gap-1.5">
          <Link to="/r/$id/$chapter" params={{ id, chapter: String(data.nav.prev) }}>
            <ChevronLeft className="size-3.5" />
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
        <Button variant="ghost" asChild className="gap-1.5">
          <Link to="/r/$id/$chapter" params={{ id, chapter: String(data.nav.next) }}>
            下一章
            <ChevronRight className="size-3.5" />
          </Link>
        </Button>
      ) : (
        <span />
      )}
    </nav>
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
  const phaseLabel =
    progress.phase === "translating"
      ? "翻译中"
      : progress.phase === "analyzing"
        ? "预读分析中"
        : progress.phase === "fetching"
          ? "抓取中"
          : progress.phase === "parsing"
            ? "解析中"
            : progress.phase === "queued"
              ? "排队中"
              : progress.phase === "error"
                ? "出错"
                : progress.phase;
  return (
    <div className="mt-6 rounded-xl border border-border p-3 space-y-2">
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
              className="gap-1"
              disabled={retrying}
              onClick={onRetryChapter}
            >
              <RotateCcw className="size-3" />
              重试本章失败 ({chapterErrorCount})
            </Button>
          )}
          {hasGlobalErrors && (
            <Button
              variant="ghost"
              size="sm"
              className="gap-1"
              disabled={retrying}
              onClick={onRetryAll}
            >
              <RotateCcw className="size-3" />
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
  const Tag = pair.type === "h2" ? "h2" : pair.type === "h3" ? "h3" : "div";
  const heading = pair.type === "h2" || pair.type === "h3";
  if (pair.type === "hr")
    return <hr className="my-8 border-t border-border" />;

  return (
    <div
      ref={ref}
      data-block-id={pair.id}
      data-status={pair.status}
      className={cn("group", heading && "mt-12 mb-6")}
    >
      {showEn && (
        <Tag
          className={
            heading
              ? "font-sans text-[clamp(1.3rem,5vw,2rem)] font-semibold leading-tight tracking-tight"
              : ""
          }
          dangerouslySetInnerHTML={{ __html: pair.en }}
        />
      )}
      {pair.status === "done" && pair.zh && (
        <div
          className={cn("zh-shadow", showEn && "mt-1.5")}
          dangerouslySetInnerHTML={{ __html: pair.zh }}
        />
      )}
      {pair.status === "pending" && (
        <div className="zh-shadow mt-1.5 italic opacity-50">翻译中…</div>
      )}
      {pair.status === "error" && (
        <div className="mt-1.5 flex items-center gap-2 text-[13px] text-destructive">
          <X className="size-3.5" />
          <span title={pair.error ?? ""}>翻译失败</span>
          {canRetry && (
            <Button
              variant="ghost"
              size="sm"
              className="h-6 gap-1 px-2 text-[11px]"
              onClick={() => onRetry(pair.id)}
            >
              <RotateCcw className="size-3" />
              重试
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
