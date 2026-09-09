import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Activity,
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Keyboard,
  Languages,
  ListOrdered,
  RotateCcw,
  Settings as SettingsIcon,
  Type,
} from "lucide-react";
import type { ChapterView, Progress } from "@ao3hub/shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { api, subscribeStream } from "../lib/api";
import { useAuth } from "../lib/auth";
import {
  applyReaderSettings,
  DEFAULT_READER_SETTINGS,
  loadReaderSettings,
  loadReadingPosition,
  READER_LIMITS,
  saveReaderSettings,
  saveReadingPosition,
  type ReaderDefaults,
  type ReaderSettings,
  type ReaderView,
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

const VIEW_OPTIONS: { value: ReaderView; label: string; hint: string }[] = [
  { value: "bilingual", label: "双语", hint: "原文在上，译文在下" },
  { value: "zh", label: "仅中文", hint: "只显示译文" },
  { value: "en", label: "仅原文", hint: "只显示英文原文" },
];

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ["←"], label: "上一章" },
  { keys: ["→"], label: "下一章" },
  { keys: ["T"], label: "章节目录" },
  { keys: ["S"], label: "阅读设置" },
  { keys: ["E"], label: "切换双语 / 仅中文" },
  { keys: ["Esc"], label: "关闭当前面板" },
];

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
  const [liveProgress, setLiveProgress] = useState<Progress | null>(null);
  const settingsTriggerRef = useRef<HTMLButtonElement>(null);
  const tocTriggerRef = useRef<HTMLButtonElement>(null);
  const statusTriggerRef = useRef<HTMLButtonElement>(null);
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
      setChromeVisible(y < 96 || y < lastY);
      lastY = y;
      latestRatio = ratio;
      persist(ratio);
    };

    const onScroll = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(measure);
    };

    measure();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
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
      toast.success("已重新入队");
      qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
      qc.invalidateQueries({ queryKey: ["stories"] });
    },
    onError: (retryError) =>
      toast.error("重试失败", { description: retryError.message }),
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
          event.preventDefault();
          setTocOpen(true);
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
  }, [anyPanelOpen, data?.nav.prev, data?.nav.next, goToChapter]);

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
        className="mx-auto flex flex-col gap-6 pt-32"
        style={{ width: "min(var(--reader-measure), calc(100vw - 32px))" }}
      >
        <Skeleton className="h-14 w-3/4" />
        <Skeleton className="h-5 w-1/3" />
        <Separator className="my-4" />
        {Array.from({ length: 10 }).map((_, index) => (
          <Skeleton
            key={index}
            className="h-5"
            style={{ width: `${72 + ((index * 13) % 28)}%` }}
          />
        ))}
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

  const titleEn = data.chapter.titleEn ?? data.meta.title;
  const rawChineseTitle =
    data.meta.chineseTitle ?? data.chapter.titleZh ?? undefined;
  // Some imports carry an untranslated `titleZh` that just repeats the English
  // title; echoing it under the heading reads like a rendering bug.
  const chineseTitle =
    rawChineseTitle && rawChineseTitle !== titleEn
      ? rawChineseTitle
      : undefined;
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
      <div
        className="pointer-events-none fixed inset-x-0 top-0 z-50 h-0.5"
        aria-hidden
      >
        <div
          className="h-full bg-primary transition-[width] duration-150"
          style={{ width: `${scrollProgress * 100}%` }}
        />
      </div>

      <ReaderTopbar
        visible={chromeVisible || anyPanelOpen}
        title={titleEn}
        subtitle={[chineseTitle, data.meta.author].filter(Boolean).join(" · ")}
        progress={scrollProgress}
        chapterIndex={chapterIndex}
        total={total ?? 1}
        totalDigits={totalDigits}
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
        settingsOpen={settingsOpen}
        settingsTriggerRef={settingsTriggerRef}
        onToggleSettings={() => setSettingsOpen((v) => !v)}
        tocOpen={tocOpen}
        tocTriggerRef={tocTriggerRef}
        onToggleToc={() => setTocOpen((v) => !v)}
        onOpenStatus={() => setStatusOpen(true)}
        statusTriggerRef={statusTriggerRef}
      />

      <Sheet open={settingsOpen} onOpenChange={setSettingsOpen}>
        <SettingsDrawer
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
        className="mx-auto pb-32 pt-28 sm:pt-24"
        style={{ width: "min(var(--reader-measure), calc(100vw - 32px))" }}
      >
        <header className="mb-12 border-b pb-8">
          <h1 className="cn-font-heading text-[clamp(2rem,6vw,3.4rem)] leading-[1.02] font-semibold tracking-tight break-words [overflow-wrap:anywhere]">
            {titleEn}
          </h1>
          {chineseTitle && (
            <p className="mt-4 text-base text-muted-foreground">
              {chineseTitle}
            </p>
          )}
          <div className="mt-4 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            {data.meta.author && (
              <Badge variant="outline">{data.meta.author}</Badge>
            )}
            {data.nav.total > 1 && (
              <Badge variant="outline">
                第 {chapterIndex + 1} / {data.nav.total} 章
              </Badge>
            )}
            {data.chapter.titleEn && data.nav.total > 1 && (
              <span className="truncate">{data.chapter.titleEn}</span>
            )}
          </div>
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

        <div
          className={cn(
            "prose-reader flex flex-col gap-[1.28em]",
            settings.focus && "reader-focus",
          )}
        >
          {data.chapter.pairs.map((p) => (
            <Pair
              key={p.id}
              pair={p}
              view={settings.view}
              canRetry={!!user}
              onRetry={(blockId) =>
                retryFailed.mutate({ blockIds: [blockId], chapterIndex })
              }
            />
          ))}
        </div>

        <ChapterNav data={data} id={id} chapterIndex={chapterIndex} />
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

function ReaderTopbar(props: {
  visible: boolean;
  title: string;
  subtitle?: string;
  progress: number;
  chapterIndex: number;
  total: number;
  totalDigits: number;
  onPrev?: () => void;
  onNext?: () => void;
  view: ReaderView;
  onViewChange: (view: ReaderView) => void;
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
    <div
      className={cn(
        "fixed inset-x-0 top-0 z-40 border-b bg-background/85 backdrop-blur-md transition-transform duration-200",
        props.visible ? "translate-y-0" : "-translate-y-full",
      )}
    >
      <div className="mx-auto flex h-14 w-full max-w-5xl items-center gap-2 px-3 sm:px-4">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              asChild
              aria-label="返回书架"
            >
              <Link to="/">
                <ChevronLeft />
              </Link>
            </Button>
          </TooltipTrigger>
          <TooltipContent>返回书架</TooltipContent>
        </Tooltip>

        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium leading-tight">
            {props.title}
          </p>
          {props.subtitle && (
            <p className="truncate text-xs leading-tight text-muted-foreground">
              {props.subtitle}
            </p>
          )}
        </div>

        <span className="hidden shrink-0 text-xs tabular-nums text-muted-foreground sm:inline">
          {Math.round(props.progress * 100)}%
        </span>

        <div className="flex shrink-0 items-center gap-0.5">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={props.onPrev}
                disabled={!props.onPrev}
                aria-label="上一章"
              >
                <ChevronLeft />
              </Button>
            </TooltipTrigger>
            <TooltipContent className="flex items-center gap-1.5">
              上一章 <Kbd>←</Kbd>
            </TooltipContent>
          </Tooltip>
          <span className="text-xs tabular-nums text-muted-foreground">
            {String(props.chapterIndex + 1).padStart(props.totalDigits, "0")}/
            {props.total}
          </span>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={props.onNext}
                disabled={!props.onNext}
                aria-label="下一章"
              >
                <ChevronRight />
              </Button>
            </TooltipTrigger>
            <TooltipContent className="flex items-center gap-1.5">
              下一章 <Kbd>→</Kbd>
            </TooltipContent>
          </Tooltip>
        </div>

        <Separator
          orientation="vertical"
          className="mx-1 hidden h-5 self-center sm:block"
        />

        <ToggleGroup
          type="single"
          value={props.view}
          onValueChange={(value) =>
            value && props.onViewChange(value as ReaderView)
          }
          variant="outline"
          size="sm"
          spacing={0}
          className="hidden md:flex"
          aria-label="显示语言"
        >
          {VIEW_OPTIONS.map((option) => (
            <ToggleGroupItem key={option.value} value={option.value}>
              {option.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>

        <div className="flex shrink-0 items-center gap-0.5">
          {props.total > 1 && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  ref={props.tocTriggerRef}
                  variant={props.tocOpen ? "secondary" : "ghost"}
                  size="icon-sm"
                  onClick={props.onToggleToc}
                  aria-label="章节目录"
                >
                  <ListOrdered />
                </Button>
              </TooltipTrigger>
              <TooltipContent className="flex items-center gap-1.5">
                章节目录 <Kbd>T</Kbd>
              </TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                ref={props.statusTriggerRef}
                variant="ghost"
                size="icon-sm"
                onClick={props.onOpenStatus}
                aria-label="翻译状态"
              >
                <Activity />
              </Button>
            </TooltipTrigger>
            <TooltipContent>翻译状态</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                ref={props.settingsTriggerRef}
                variant={props.settingsOpen ? "secondary" : "ghost"}
                size="icon-sm"
                onClick={props.onToggleSettings}
                aria-label="阅读设置"
              >
                <SettingsIcon />
              </Button>
            </TooltipTrigger>
            <TooltipContent className="flex items-center gap-1.5">
              阅读设置 <Kbd>S</Kbd>
            </TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  );
}

function SettingsDrawer({
  settings,
  setSettings,
  defaults,
  onCloseAutoFocus,
}: {
  settings: ReaderSettings;
  setSettings: (updater: (current: ReaderSettings) => ReaderSettings) => void;
  defaults: ReaderDefaults;
  onCloseAutoFocus: (event: Event) => void;
}) {
  const patch = (next: Partial<ReaderSettings>) =>
    setSettings((current) => ({ ...current, ...next }));

  return (
    <SheetContent
      side="right"
      onCloseAutoFocus={onCloseAutoFocus}
      className="w-full gap-0 overflow-y-auto sm:max-w-sm"
    >
      <SheetHeader>
        <SheetTitle>阅读设置</SheetTitle>
        <SheetDescription>排版偏好保存在这台设备上。</SheetDescription>
      </SheetHeader>

      <div className="flex flex-col gap-6 px-4 pb-6">
        <FieldSet>
          <FieldLegend variant="label" className="flex items-center gap-2">
            <Languages className="size-4" />
            显示语言
          </FieldLegend>
          <ToggleGroup
            type="single"
            value={settings.view}
            onValueChange={(value) =>
              value && patch({ view: value as ReaderView })
            }
            variant="outline"
            spacing={0}
            className="w-full"
            aria-label="显示语言"
          >
            {VIEW_OPTIONS.map((option) => (
              <ToggleGroupItem
                key={option.value}
                value={option.value}
                className="flex-1"
              >
                {option.label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <FieldDescription>
            {VIEW_OPTIONS.find((o) => o.value === settings.view)?.hint}
          </FieldDescription>
        </FieldSet>

        <Separator />

        <FieldSet>
          <FieldLegend variant="label" className="flex items-center gap-2">
            <Type className="size-4" />
            排版
          </FieldLegend>
          <div className="flex flex-col gap-5">
            <ReaderSlider
              label="字号"
              value={settings.font}
              min={READER_LIMITS.font.min}
              max={READER_LIMITS.font.max}
              step={READER_LIMITS.font.step}
              format={(v) => `${v}px`}
              onChange={(v) => patch({ font: v })}
            />
            <ReaderSlider
              label="中文比例"
              value={settings.zh}
              min={READER_LIMITS.zh.min}
              max={READER_LIMITS.zh.max}
              step={READER_LIMITS.zh.step}
              format={(v) => `${Math.round(v * 100)}%`}
              onChange={(v) => patch({ zh: Number(v.toFixed(2)) })}
            />
            <ReaderSlider
              label="栏宽"
              value={settings.measure}
              min={READER_LIMITS.measure.min}
              max={READER_LIMITS.measure.max}
              step={READER_LIMITS.measure.step}
              format={(v) => `${v}px`}
              onChange={(v) => patch({ measure: v })}
            />
          </div>
        </FieldSet>

        <Separator />

        <FieldLabel htmlFor="reader-focus">
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>专注模式</FieldTitle>
              <FieldDescription>
                淡出其他段落，只保留鼠标所在的一段。
              </FieldDescription>
            </FieldContent>
            <Switch
              id="reader-focus"
              checked={settings.focus}
              onCheckedChange={(checked) => patch({ focus: checked })}
            />
          </Field>
        </FieldLabel>

        <Button
          variant="outline"
          size="sm"
          className="self-start"
          onClick={() =>
            setSettings((current) => ({ ...current, ...defaults }))
          }
        >
          <RotateCcw data-icon="inline-start" />
          恢复默认排版
        </Button>

        <Separator />

        <section className="flex flex-col gap-2">
          <h3 className="flex items-center gap-2 text-sm font-medium">
            <Keyboard className="size-4" />
            键盘快捷键
          </h3>
          <dl className="flex flex-col gap-1.5 text-sm">
            {SHORTCUTS.map((shortcut) => (
              <div
                key={shortcut.label}
                className="flex items-center justify-between gap-4"
              >
                <dt className="text-muted-foreground">{shortcut.label}</dt>
                <dd>
                  <KbdGroup>
                    {shortcut.keys.map((key) => (
                      <Kbd key={key}>{key}</Kbd>
                    ))}
                  </KbdGroup>
                </dd>
              </div>
            ))}
          </dl>
        </section>
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
        <span className="text-xs tabular-nums text-muted-foreground">
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
  const activeRef = useRef<HTMLAnchorElement>(null);

  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "center" });
  }, []);

  const items = Array.from({ length: data.nav.total }, (_, i) => i);
  return (
    <SheetContent
      side="left"
      onCloseAutoFocus={onCloseAutoFocus}
      className="w-full gap-0 overflow-y-auto sm:max-w-sm"
    >
      <SheetHeader>
        <SheetTitle>章节目录</SheetTitle>
        <SheetDescription>
          共 {data.nav.total} 章 · 当前为第 {chapterIndex + 1} 章
        </SheetDescription>
      </SheetHeader>
      <ul className="flex flex-col gap-0.5 px-2 pb-6">
        {items.map((i) => {
          const active = i === chapterIndex;
          return (
            <li key={i}>
              <Link
                ref={active ? activeRef : undefined}
                to="/r/$id/$chapter"
                params={{ id: data.meta.id, chapter: String(i) }}
                onClick={onClose}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm outline-none transition-colors hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50",
                  active && "bg-accent font-medium text-accent-foreground",
                )}
              >
                <span className="w-8 shrink-0 text-xs tabular-nums text-muted-foreground">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <span className="flex-1 truncate">
                  {active && data.chapter.titleEn
                    ? data.chapter.titleEn
                    : `Chapter ${i + 1}`}
                </span>
              </Link>
            </li>
          );
        })}
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
      <nav className="flex items-center justify-between gap-4 pt-8">
        {data.nav.prev !== undefined ? (
          <Button variant="outline" asChild>
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
        <span className="text-sm tabular-nums text-muted-foreground">
          {chapterIndex + 1} / {data.nav.total}
        </span>
        {data.nav.next !== undefined ? (
          <Button variant="outline" asChild>
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
  // A failed run can leave every block `pending` (e.g. the LLM key was never
  // configured), so the phase itself has to offer the way out.
  const failed = progress.phase === "error";
  const showRetryRow =
    canRetryAll &&
    (failed || (total > 0 && (showChapterRetry || hasGlobalErrors)));

  return (
    <div className="mt-6 flex flex-col gap-2.5 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span
          className={cn(
            "text-xs",
            failed ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {PHASE_LABEL[progress.phase]}
          {progress.message ? ` · ${progress.message}` : null}
        </span>
        <TranslateProgressLegend progress={progress} />
      </div>
      <TranslateProgressBar progress={progress} />
      {showRetryRow && (
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
          {(hasGlobalErrors || failed) && (
            <Button
              variant={failed && !showChapterRetry ? "outline" : "ghost"}
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
        </div>
      )}
    </div>
  );
}

function Pair({
  pair,
  view,
  canRetry,
  onRetry,
}: {
  pair: ChapterView["chapter"]["pairs"][number];
  view: ReaderView;
  canRetry: boolean;
  onRetry: (blockId: string) => void;
}) {
  const heading = pair.type === "h2" || pair.type === "h3";
  if (pair.type === "hr") return <Separator className="my-8" />;

  const translated = pair.status === "done" && pair.zh;
  // "Chinese only" still falls back to the source while a block is untranslated
  // — an empty gap reads as a rendering bug.
  const showEn = view === "en" || view === "bilingual" || !translated;
  const showZh = view !== "en" && !!translated;

  return (
    <div
      data-block-id={pair.id}
      data-status={pair.status}
      className={cn("reader-block group", heading && "mt-12 mb-6")}
    >
      {showEn && <RichHTML type={pair.type} html={pair.en} />}
      {showZh && (
        <RichHTML
          type={pair.type}
          html={pair.zh!}
          className={cn("zh-shadow", showEn && "mt-1.5")}
        />
      )}
      {pair.status === "pending" && view !== "en" && (
        <Skeleton className="mt-1.5 h-5 w-3/4" />
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
