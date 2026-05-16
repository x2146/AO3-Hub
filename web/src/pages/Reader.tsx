import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { ChapterView } from "@ao3hub/shared";
import { api, subscribeStream } from "../lib/api";
import {
  applyReaderSettings,
  loadReaderSettings,
  READER_LIMITS,
  saveReaderSettings,
  type ReaderSettings,
} from "../lib/reader-settings";

export function Reader() {
  const { id, chapter } = useParams({ from: "/r/$id/$chapter" });
  const chapterIndex = Number(chapter);
  const navigate = useNavigate();
  const qc = useQueryClient();

  const [settings, setSettings] = useState<ReaderSettings>(() => loadReaderSettings());
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [tocOpen, setTocOpen] = useState(false);
  const [progress, setProgress] = useState(0);
  const [showAllEn, setShowAllEn] = useState(true);

  useEffect(() => {
    applyReaderSettings(settings);
    saveReaderSettings(settings);
  }, [settings]);

  useEffect(() => {
    const onScroll = () => {
      const h = document.documentElement.scrollHeight - window.innerHeight;
      setProgress(h > 0 ? Math.min(1, Math.max(0, window.scrollY / h)) : 0);
    };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, [chapterIndex]);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["chapter", id, chapterIndex],
    queryFn: () => api.getChapter(id, chapterIndex),
  });

  const total = data?.nav.total;
  const totalDigits = useMemo(
    () => (total != null ? String(total).length : 1),
    [total],
  );

  useEffect(() => {
    if (!data) return;
    if (data.progress.phase === "ready") return;
    const unsub = subscribeStream(id, (event) => {
      if (event.type === "block-done" && event.chapterIndex === chapterIndex) {
        qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
      } else if (event.type === "phase") {
        qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
        qc.invalidateQueries({ queryKey: ["stories"] });
      } else if (event.type === "chapter-done") {
        qc.invalidateQueries({ queryKey: ["chapter", id, chapterIndex] });
      }
    });
    return unsub;
  }, [id, chapterIndex, data?.progress.phase, qc]);

  useEffect(() => {
    window.scrollTo({ top: 0 });
  }, [chapterIndex]);

  if (isLoading) {
    return (
      <div className="py-32 text-center">
        <p className="text-muted">载入章节…</p>
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="py-32 text-center space-y-3">
        <p className="text-red-500">载入失败：{(error as Error)?.message ?? "unknown"}</p>
        <Link to="/" className="btn btn-ghost">
          返回书架
        </Link>
      </div>
    );
  }

  const titleEn = data.chapter.titleEn ?? data.meta.title;
  const chineseTitle =
    data.meta.chineseTitle ?? data.chapter.titleZh ?? undefined;

  return (
    <>
      <ReaderTopbar
        title={titleEn}
        subtitle={[chineseTitle, data.meta.author].filter(Boolean).join(" · ")}
        progress={progress}
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
        onToggleSettings={() => setSettingsOpen((v) => !v)}
        tocOpen={tocOpen}
        onToggleToc={() => setTocOpen((v) => !v)}
      />

      {settingsOpen && (
        <SettingsDrawer
          settings={settings}
          setSettings={setSettings}
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
        <header className="mb-12 border-b rule pb-8">
          <h1 className="text-[clamp(2rem,6vw,3.6rem)] font-semibold leading-[0.98] tracking-tight">
            {titleEn}
          </h1>
          {chineseTitle && (
            <p className="text-muted mt-4 text-[16px]">
              {chineseTitle}
            </p>
          )}
          {data.chapter.titleEn && data.nav.total > 1 && (
            <p className="text-muted mt-2 text-[13px] font-mono">
              CH {chapterIndex + 1}/{data.nav.total} ·{" "}
              {data.chapter.titleEn}
            </p>
          )}
          <ProgressBar progress={data.progress} />
        </header>

        <div className="prose-reader space-y-[1.28em]">
          {data.chapter.pairs.map((p) => (
            <Pair
              key={p.id}
              pair={p}
              showEn={showAllEn}
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
          className="h-full bg-[rgb(var(--accent))] transition-[width] duration-100"
          style={{ width: `${progress * 100}%` }}
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
    <div className="fixed left-1/2 top-3 z-40 flex w-[min(820px,calc(100vw-24px))] -translate-x-1/2 items-center gap-3 rounded-full surface rule border px-3 py-1.5 shadow-[0_18px_44px_rgba(17,24,39,0.12)]">
      <Link to="/" className="btn btn-ghost text-[12px]">
        ← 目录
      </Link>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13px] font-semibold leading-tight">
          {props.title}
        </p>
        {props.subtitle && (
          <p className="text-muted truncate text-[11px] leading-tight">
            {props.subtitle}
          </p>
        )}
      </div>
      <div className="flex items-center gap-1 text-[12px] font-mono text-muted">
        <button
          type="button"
          className="btn btn-ghost px-2"
          onClick={props.onPrev}
          disabled={!props.onPrev}
          aria-label="上一章"
        >
          ‹
        </button>
        <span className="tabular-nums">
          {String(props.chapterIndex + 1).padStart(props.totalDigits, "0")}/
          {props.total}
        </span>
        <button
          type="button"
          className="btn btn-ghost px-2"
          onClick={props.onNext}
          disabled={!props.onNext}
          aria-label="下一章"
        >
          ›
        </button>
      </div>
      <span className="text-muted tabular-nums text-[11px] font-mono">
        {Math.round(props.progress * 100)}%
      </span>
      {props.total > 1 && (
        <button
          type="button"
          className={`btn btn-ghost text-[12px] ${props.tocOpen ? "bg-[rgb(var(--ink)/0.08)]" : ""}`}
          onClick={props.onToggleToc}
        >
          章节
        </button>
      )}
      <button
        type="button"
        className={`btn btn-ghost text-[12px] ${props.settingsOpen ? "bg-[rgb(var(--ink)/0.08)]" : ""}`}
        onClick={props.onToggleSettings}
      >
        设置
      </button>
    </div>
  );
}

function SettingsDrawer({
  settings,
  setSettings,
  showAllEn,
  setShowAllEn,
  onClose,
}: {
  settings: ReaderSettings;
  setSettings: (s: ReaderSettings) => void;
  showAllEn: boolean;
  setShowAllEn: (v: boolean) => void;
  onClose: () => void;
}) {
  return (
    <div
      role="dialog"
      className="fixed top-[64px] left-1/2 z-40 w-[min(560px,calc(100vw-24px))] -translate-x-1/2 rounded-3xl surface rule border p-5 shadow-[0_28px_60px_rgba(17,24,39,0.18)]"
    >
      <div className="flex items-center justify-between">
        <p className="text-[13px] font-semibold tracking-wider uppercase text-muted">
          阅读设置
        </p>
        <button type="button" className="btn btn-ghost text-[12px]" onClick={onClose}>
          关闭
        </button>
      </div>
      <div className="mt-4 grid gap-3">
        <Slider
          label="字号"
          value={settings.font}
          unit="px"
          min={READER_LIMITS.font.min}
          max={READER_LIMITS.font.max}
          step={READER_LIMITS.font.step}
          onChange={(v) => setSettings({ ...settings, font: v })}
        />
        <Slider
          label="中文比例"
          value={settings.zh}
          unit=""
          formatter={(v) => `${Math.round(v * 100)}%`}
          min={READER_LIMITS.zh.min}
          max={READER_LIMITS.zh.max}
          step={READER_LIMITS.zh.step}
          onChange={(v) => setSettings({ ...settings, zh: Number(v.toFixed(2)) })}
        />
        <Slider
          label="栏宽"
          value={settings.measure}
          unit="px"
          min={READER_LIMITS.measure.min}
          max={READER_LIMITS.measure.max}
          step={READER_LIMITS.measure.step}
          onChange={(v) => setSettings({ ...settings, measure: v })}
        />
        <label className="flex items-center justify-between text-[13px]">
          <span>显示原文</span>
          <input
            type="checkbox"
            checked={showAllEn}
            onChange={(e) => setShowAllEn(e.target.checked)}
          />
        </label>
        <button
          type="button"
          className="btn btn-ghost text-[12px] mt-1"
          onClick={() =>
            setSettings({ font: 17, zh: 0.96, measure: 760 })
          }
        >
          恢复默认
        </button>
      </div>
    </div>
  );
}

function Slider(props: {
  label: string;
  value: number;
  unit: string;
  formatter?: (v: number) => string;
  min: number;
  max: number;
  step: number;
  onChange: (v: number) => void;
}) {
  const display = props.formatter ? props.formatter(props.value) : `${props.value}${props.unit}`;
  return (
    <div className="flex items-center gap-4">
      <span className="text-muted w-[80px] text-[12px]">{props.label}</span>
      <input
        type="range"
        min={props.min}
        max={props.max}
        step={props.step}
        value={props.value}
        onChange={(e) => props.onChange(Number(e.target.value))}
        className="flex-1 accent-[rgb(var(--accent))]"
      />
      <span className="text-muted w-[64px] text-right font-mono text-[12px]">{display}</span>
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
      className="fixed top-[64px] left-1/2 z-40 w-[min(560px,calc(100vw-24px))] -translate-x-1/2 rounded-3xl surface rule border p-3 shadow-[0_28px_60px_rgba(17,24,39,0.18)] max-h-[60vh] overflow-y-auto"
    >
      <div className="flex items-center justify-between px-2 py-1">
        <p className="text-[13px] font-semibold tracking-wider uppercase text-muted">
          章节目录
        </p>
        <button type="button" className="btn btn-ghost text-[12px]" onClick={onClose}>
          关闭
        </button>
      </div>
      <ul className="mt-2">
        {items.map((i) => (
          <li key={i}>
            <Link
              to="/r/$id/$chapter"
              params={{ id: data.meta.id, chapter: String(i) }}
              onClick={onClose}
              className={`flex items-center justify-between rounded-xl px-3 py-2 text-[13px] hover:bg-[rgb(var(--ink)/0.05)] ${
                i === chapterIndex ? "bg-[rgb(var(--accent)/0.1)]" : ""
              }`}
            >
              <span className="font-mono text-muted">
                {String(i + 1).padStart(2, "0")}
              </span>
              <span className="ml-3 flex-1 truncate">
                Chapter {i + 1}
              </span>
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
    <nav className="mt-20 flex items-center justify-between border-t rule pt-8 text-[13px] text-muted">
      {data.nav.prev !== undefined ? (
        <Link
          to="/r/$id/$chapter"
          params={{ id, chapter: String(data.nav.prev) }}
          className="btn btn-ghost"
        >
          ← 上一章
        </Link>
      ) : (
        <span />
      )}
      <span className="font-mono tabular-nums">
        {chapterIndex + 1}/{data.nav.total}
      </span>
      {data.nav.next !== undefined ? (
        <Link
          to="/r/$id/$chapter"
          params={{ id, chapter: String(data.nav.next) }}
          className="btn btn-ghost"
        >
          下一章 →
        </Link>
      ) : (
        <span />
      )}
    </nav>
  );
}

function ProgressBar({ progress }: { progress: ChapterView["progress"] }) {
  if (progress.phase === "ready") return null;
  const pct = progress.totalBlocks
    ? Math.round((progress.doneBlocks / progress.totalBlocks) * 100)
    : 0;
  return (
    <div className="mt-6 rounded-xl border rule p-3">
      <div className="flex items-center justify-between text-[12px]">
        <span className="text-muted">
          {progress.phase === "translating"
            ? "翻译中"
            : progress.phase === "fetching"
              ? "抓取中"
              : progress.phase === "parsing"
                ? "解析中"
                : progress.phase === "queued"
                  ? "排队中"
                  : progress.phase}
        </span>
        <span className="font-mono">
          {progress.doneBlocks}/{progress.totalBlocks} · {pct}%
        </span>
      </div>
      <div className="mt-2 h-1 overflow-hidden rounded-full bg-[rgb(var(--ink)/0.08)]">
        <div
          className="h-full bg-[rgb(var(--accent))] transition-[width] duration-300"
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

function Pair({
  pair,
  showEn,
  onRetry,
}: {
  pair: ChapterView["chapter"]["pairs"][number];
  showEn: boolean;
  onRetry: (blockId: string) => void;
}) {
  const Tag = pair.type === "h2" ? "h2" : pair.type === "h3" ? "h3" : "div";
  const heading = pair.type === "h2" || pair.type === "h3";
  if (pair.type === "hr") return <hr className="my-8 rule border-t" />;

  return (
    <div
      ref={useRef<HTMLDivElement>(null)}
      data-block-id={pair.id}
      data-status={pair.status}
      className={`group ${heading ? "mt-12 mb-6" : ""}`}
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
          className={`${showEn ? "mt-1.5" : ""} zh-shadow`}
          dangerouslySetInnerHTML={{ __html: pair.zh }}
        />
      )}
      {pair.status === "pending" && (
        <div className="zh-shadow mt-1.5 italic opacity-50">翻译中…</div>
      )}
      {pair.status === "error" && (
        <div className="mt-1.5 flex items-center gap-2 text-[13px] text-red-500">
          <span title={pair.error ?? ""}>× 翻译失败</span>
          <button
            type="button"
            className="btn btn-ghost text-[11px] py-0.5"
            onClick={() => onRetry(pair.id)}
          >
            重试
          </button>
        </div>
      )}
    </div>
  );
}
