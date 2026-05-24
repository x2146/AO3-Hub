import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AlertCircle,
  CheckCircle2,
  ChevronDown,
  Clock,
  Cpu,
  FileText,
  RefreshCw,
  RotateCcw,
  Sparkles,
  Trash2,
  X,
  Zap,
} from "lucide-react";
import type {
  LlmCallEvent,
  LlmCallStage,
  RequestSample,
  StageStats,
  TranslationStatusView,
} from "@ao3hub/shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { api, subscribeStream } from "../lib/api";
import { useAuth } from "../lib/auth";

type Props = {
  storyID: string;
  open: boolean;
  onClose: () => void;
};

const STAGE_LABEL: Record<LlmCallStage, string> = {
  "analysis-chapter": "分章预读",
  "analysis-merge": "归并",
  "analysis-full": "全文预读",
  "translate-batch": "翻译批次",
};

const STAGE_COLOR: Record<LlmCallStage, string> = {
  "analysis-chapter": "bg-accent/15 text-accent",
  "analysis-merge": "bg-accent/15 text-accent",
  "analysis-full": "bg-accent/15 text-accent",
  "translate-batch": "bg-success/15 text-success",
};

export function TranslationStatusPanel({ storyID, open, onClose }: Props) {
  const qc = useQueryClient();
  const { user } = useAuth();
  const [autoRefresh, setAutoRefresh] = useState(true);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["translation-status", storyID],
    queryFn: () => api.getTranslationStatus(storyID),
    enabled: open,
    refetchInterval: autoRefresh && open ? 2500 : false,
  });

  useEffect(() => {
    if (!open) return;
    const unsub = subscribeStream(storyID, (event) => {
      if (event.type === "llm-call" || event.type === "phase") {
        qc.invalidateQueries({ queryKey: ["translation-status", storyID] });
      }
    });
    return unsub;
  }, [open, storyID, qc]);

  const resetStats = useMutation({
    mutationFn: () => api.resetTranslationStats(storyID),
    onSuccess: () => refetch(),
  });

  const reanalyze = useMutation({
    mutationFn: () => api.reanalyze(storyID),
    onSuccess: () => {
      refetch();
      qc.invalidateQueries({ queryKey: ["stories"] });
    },
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    if (open) window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <div
        className="absolute inset-0 bg-black/30 backdrop-blur-[2px]"
        onClick={onClose}
        aria-hidden
      />
      <aside
        role="dialog"
        aria-label="翻译状态"
        className="relative h-full w-full max-w-[640px] overflow-y-auto border-l border-border bg-card shadow-[0_28px_60px_rgba(17,24,39,0.18)] surface"
      >
        <header className="sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-border bg-card/95 backdrop-blur px-5 py-3">
          <div className="flex items-center gap-2 min-w-0">
            <Activity className="size-4 text-accent shrink-0" />
            <div className="min-w-0">
              <p className="text-[13px] font-semibold uppercase tracking-wider text-muted-foreground">
                翻译状态
              </p>
              <p className="text-[11px] text-muted-foreground font-mono truncate">
                {storyID}
                {data?.mode === "refined" && (
                  <span className="ml-2 text-accent">refined</span>
                )}
              </p>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              className="gap-1"
              onClick={() => setAutoRefresh((v) => !v)}
              aria-label={autoRefresh ? "暂停自动刷新" : "开启自动刷新"}
            >
              <RefreshCw
                className={cn(
                  "size-3.5",
                  autoRefresh && isFetching && "animate-spin",
                )}
              />
              {autoRefresh ? "Live" : "Paused"}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={onClose}
              aria-label="关闭"
            >
              <X className="size-3.5" />
            </Button>
          </div>
        </header>

        <div className="px-5 py-4">
          {isLoading && (
            <p className="text-muted-foreground text-[13px]">载入状态…</p>
          )}
          {error && (
            <p className="text-destructive text-[13px]">
              加载失败：{(error as Error).message}
            </p>
          )}
          {data && (
            <Tabs defaultValue="overview">
              <TabsList className="grid grid-cols-5 w-full">
                <TabsTrigger value="overview">概览</TabsTrigger>
                <TabsTrigger value="context">预读</TabsTrigger>
                <TabsTrigger value="samples">Ctx</TabsTrigger>
                <TabsTrigger value="events">调用</TabsTrigger>
                <TabsTrigger value="errors">错误</TabsTrigger>
              </TabsList>

              <TabsContent value="overview" className="mt-5">
                <OverviewTab
                  data={data}
                  canManage={!!user}
                  onReset={() => {
                    if (confirm("重置该作品的全部翻译统计？")) {
                      resetStats.mutate();
                    }
                  }}
                  resetting={resetStats.isPending}
                  onReanalyze={() => {
                    if (
                      confirm(
                        "重新预读分析并翻译？将清空已生成的 context 并以精翻模式重新入队。",
                      )
                    ) {
                      reanalyze.mutate();
                    }
                  }}
                  reanalyzing={reanalyze.isPending}
                />
              </TabsContent>

              <TabsContent value="context" className="mt-5">
                <ContextTab data={data} />
              </TabsContent>

              <TabsContent value="samples" className="mt-5">
                <SamplesTab samples={data.samples} canSeeRaw={!!user} />
              </TabsContent>

              <TabsContent value="events" className="mt-5">
                <EventsTab events={data.events} />
              </TabsContent>

              <TabsContent value="errors" className="mt-5">
                <ErrorsTab events={data.events} />
              </TabsContent>
            </Tabs>
          )}
        </div>
      </aside>
    </div>
  );
}

function OverviewTab({
  data,
  canManage,
  onReset,
  resetting,
  onReanalyze,
  reanalyzing,
}: {
  data: TranslationStatusView;
  canManage: boolean;
  onReset: () => void;
  resetting: boolean;
  onReanalyze: () => void;
  reanalyzing: boolean;
}) {
  const total = data.stats.total;
  const successRate =
    total.calls > 0 ? Math.round((total.successes / total.calls) * 100) : 0;
  const avgTokens =
    total.calls > 0 ? Math.round(total.totalTokens / total.calls) : 0;
  const avgDuration =
    total.calls > 0 ? Math.round(total.durationMs / total.calls) : 0;

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-2 gap-3">
        <StatCard
          icon={<Cpu className="size-3.5" />}
          label="API 调用"
          value={total.calls.toLocaleString()}
          sub={`成功率 ${successRate}%`}
        />
        <StatCard
          icon={<Zap className="size-3.5" />}
          label="总 Token"
          value={total.totalTokens.toLocaleString()}
          sub={`入 ${total.promptTokens.toLocaleString()} · 出 ${total.completionTokens.toLocaleString()}`}
        />
        <StatCard
          icon={<CheckCircle2 className="size-3.5 text-success" />}
          label="成功 / 失败"
          value={`${total.successes} / ${total.failures}`}
          sub={`重试 ${total.retries}`}
        />
        <StatCard
          icon={<Clock className="size-3.5" />}
          label="均时 / 总时"
          value={formatDuration(avgDuration)}
          sub={`累计 ${formatDuration(total.durationMs)}`}
        />
      </div>

      <div>
        <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-2">
          按阶段
        </p>
        <div className="space-y-2">
          {(Object.keys(data.stats.byStage) as LlmCallStage[]).length === 0 && (
            <p className="text-[12px] text-muted-foreground">暂无调用记录</p>
          )}
          {(Object.entries(data.stats.byStage) as [LlmCallStage, StageStats][])
            .sort((a, b) => b[1].calls - a[1].calls)
            .map(([stage, stats]) => (
              <StageRow key={stage} stage={stage} stats={stats} />
            ))}
        </div>
      </div>

      <div className="flex items-center gap-2 text-[11px] text-muted-foreground font-mono">
        {data.stats.startedAt && (
          <>
            <span>首次 {formatTime(data.stats.startedAt)}</span>
            <span>·</span>
          </>
        )}
        {data.stats.lastCallAt && (
          <span>最近 {formatTime(data.stats.lastCallAt)}</span>
        )}
      </div>

      {canManage && (
        <>
          <Separator />
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              disabled={reanalyzing}
              onClick={onReanalyze}
            >
              <Sparkles className="size-3.5" />
              {reanalyzing ? "重新入队…" : "重新预读 + 翻译"}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5 text-destructive"
              disabled={resetting}
              onClick={onReset}
            >
              <Trash2 className="size-3.5" />
              {resetting ? "重置中…" : "重置统计"}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

function StatCard({
  icon,
  label,
  value,
  sub,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <div className="rounded-2xl border border-border bg-secondary/40 px-4 py-3">
      <div className="flex items-center gap-1.5 text-[11px] uppercase tracking-wider text-muted-foreground">
        {icon}
        <span>{label}</span>
      </div>
      <p className="mt-1.5 text-[22px] font-semibold tabular-nums leading-none">
        {value}
      </p>
      {sub && (
        <p className="mt-1.5 text-[11px] text-muted-foreground font-mono tabular-nums">
          {sub}
        </p>
      )}
    </div>
  );
}

function StageRow({
  stage,
  stats,
}: {
  stage: LlmCallStage;
  stats: StageStats;
}) {
  const successRate =
    stats.calls > 0 ? Math.round((stats.successes / stats.calls) * 100) : 0;
  return (
    <div className="rounded-xl border border-border px-3 py-2 text-[12px]">
      <div className="flex items-center justify-between">
        <Badge variant="outline" className={cn("font-mono", STAGE_COLOR[stage])}>
          {STAGE_LABEL[stage]}
        </Badge>
        <span className="text-muted-foreground tabular-nums font-mono">
          {stats.calls} 次 · {successRate}% · {stats.totalTokens.toLocaleString()} tok
        </span>
      </div>
      {(stats.failures > 0 || stats.retries > 0) && (
        <p className="mt-1 text-[11px] text-muted-foreground font-mono">
          {stats.failures > 0 && (
            <span className="text-destructive mr-2">失败 {stats.failures}</span>
          )}
          {stats.retries > 0 && <span>重试 {stats.retries}</span>}
        </p>
      )}
    </div>
  );
}

function ContextTab({ data }: { data: TranslationStatusView }) {
  const ctx = data.context;
  if (!ctx) {
    return (
      <div className="rounded-xl border border-dashed border-border p-6 text-center">
        <FileText className="size-5 mx-auto text-muted-foreground" />
        <p className="mt-2 text-[13px] text-muted-foreground">
          {data.mode === "refined"
            ? "预读分析尚未生成"
            : "当前为快翻模式，无预读分析"}
        </p>
      </div>
    );
  }
  return (
    <div className="space-y-4">
      {ctx.summary && (
        <Section title="全文摘要">
          <p className="text-[13px] leading-relaxed whitespace-pre-wrap">
            {ctx.summary}
          </p>
        </Section>
      )}
      {ctx.tone && (
        <Section title="风格基调">
          <p className="text-[13px] leading-relaxed">{ctx.tone}</p>
        </Section>
      )}
      {ctx.ships.length > 0 && (
        <Section title={`Ships (${ctx.ships.length})`}>
          <div className="flex flex-wrap gap-1.5">
            {ctx.ships.map((s) => (
              <Badge key={s} variant="accent" className="font-mono normal-case">
                {s}
              </Badge>
            ))}
          </div>
        </Section>
      )}
      {ctx.characters.length > 0 && (
        <Section title={`角色 (${ctx.characters.length})`}>
          <ul className="space-y-1.5">
            {ctx.characters.map((c) => (
              <li
                key={c.name}
                className="rounded-lg border border-border px-3 py-2 text-[12px]"
              >
                <p className="font-semibold">
                  {c.name}
                  {c.zh && (
                    <span className="text-muted-foreground ml-2 font-normal">
                      · {c.zh}
                    </span>
                  )}
                </p>
                {c.role && (
                  <p className="mt-0.5 text-[11px] text-muted-foreground">
                    {c.role}
                  </p>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}
      {Object.keys(ctx.glossary).length > 0 && (
        <Section title={`术语表 (${Object.keys(ctx.glossary).length})`}>
          <div className="grid grid-cols-1 gap-1 font-mono text-[12px]">
            {Object.entries(ctx.glossary).map(([k, v]) => (
              <div
                key={k}
                className="flex items-center justify-between rounded border border-border/60 px-2 py-1"
              >
                <span className="truncate">{k}</span>
                <span className="text-muted-foreground ml-3 truncate">{v}</span>
              </div>
            ))}
          </div>
        </Section>
      )}
      {ctx.chapterSummaries.length > 0 && (
        <Section title={`分章摘要 (${ctx.chapterSummaries.length})`}>
          <ol className="space-y-2">
            {ctx.chapterSummaries.map((c) => (
              <li
                key={c.index}
                className="rounded-lg border border-border px-3 py-2 text-[12px]"
              >
                <div className="flex items-baseline gap-2">
                  <span className="font-mono text-muted-foreground">
                    CH {String(c.index + 1).padStart(2, "0")}
                  </span>
                  {c.title && (
                    <span className="font-semibold truncate">{c.title}</span>
                  )}
                </div>
                <p className="mt-1 leading-relaxed">{c.summary}</p>
              </li>
            ))}
          </ol>
        </Section>
      )}
      <p className="text-[11px] text-muted-foreground font-mono">
        生成于 {formatTime(ctx.generatedAt)} · {ctx.chapterCount ?? "?"} 章
      </p>
    </div>
  );
}

function SamplesTab({
  samples,
  canSeeRaw,
}: {
  samples: Record<string, RequestSample>;
  canSeeRaw: boolean;
}) {
  const entries = Object.entries(samples) as [LlmCallStage, RequestSample][];
  if (entries.length === 0) {
    return (
      <p className="rounded-xl border border-dashed border-border p-6 text-center text-[13px] text-muted-foreground">
        尚无样本。开始翻译后会捕获每个阶段的最新一次请求。
      </p>
    );
  }
  if (!canSeeRaw) {
    return (
      <p className="rounded-xl border border-dashed border-border p-6 text-center text-[13px] text-muted-foreground">
        请求 ctx 仅登录用户可见。
      </p>
    );
  }
  return (
    <div className="space-y-3">
      {entries.map(([stage, sample]) => (
        <SampleCard key={stage} stage={stage} sample={sample} />
      ))}
    </div>
  );
}

function SampleCard({
  stage,
  sample,
}: {
  stage: LlmCallStage;
  sample: RequestSample;
}) {
  const [showSystem, setShowSystem] = useState(false);
  return (
    <div className="rounded-xl border border-border overflow-hidden">
      <div className="flex items-center justify-between bg-secondary/40 px-3 py-2 text-[12px]">
        <div className="flex items-center gap-2">
          <Badge variant="outline" className={cn("font-mono", STAGE_COLOR[stage])}>
            {STAGE_LABEL[stage]}
          </Badge>
          {sample.chapterIndex !== undefined && (
            <span className="font-mono text-muted-foreground">
              CH {String(sample.chapterIndex + 1).padStart(2, "0")}
            </span>
          )}
          {sample.blockIds && sample.blockIds.length > 0 && (
            <span className="font-mono text-muted-foreground">
              {sample.blockIds.length} blocks
            </span>
          )}
        </div>
        <span className="text-[11px] text-muted-foreground font-mono">
          {formatTime(sample.capturedAt)}
        </span>
      </div>
      <div className="px-3 py-3 space-y-3">
        <details className="group" open={showSystem} onToggle={(e) => setShowSystem((e.currentTarget as HTMLDetailsElement).open)}>
          <summary className="flex cursor-pointer items-center justify-between text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            <span>System Prompt</span>
            <ChevronDown
              className={cn(
                "size-3 transition-transform",
                showSystem && "rotate-180",
              )}
            />
          </summary>
          <pre className="mt-2 max-h-[200px] overflow-auto rounded bg-secondary/40 p-2 text-[11px] font-mono leading-relaxed whitespace-pre-wrap break-words">
            {sample.systemPrompt || "(空)"}
          </pre>
        </details>

        <div>
          <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">
            User Payload
          </p>
          <pre className="max-h-[280px] overflow-auto rounded bg-secondary/40 p-2 text-[11px] font-mono leading-relaxed whitespace-pre-wrap break-words">
            {sample.userPayload || "(空)"}
          </pre>
        </div>

        {sample.responsePreview && (
          <div>
            <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-1.5">
              Response
            </p>
            <pre className="max-h-[200px] overflow-auto rounded bg-secondary/40 p-2 text-[11px] font-mono leading-relaxed whitespace-pre-wrap break-words">
              {sample.responsePreview}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}

function EventsTab({ events }: { events: LlmCallEvent[] }) {
  const recent = useMemo(() => [...events].reverse(), [events]);
  if (recent.length === 0) {
    return (
      <p className="rounded-xl border border-dashed border-border p-6 text-center text-[13px] text-muted-foreground">
        尚无调用记录。
      </p>
    );
  }
  return (
    <ul className="space-y-1">
      {recent.map((e) => (
        <li
          key={e.id}
          className={cn(
            "rounded-lg border px-3 py-2 text-[11px] font-mono",
            e.status === "error"
              ? "border-destructive/40 bg-destructive/5"
              : "border-border",
          )}
        >
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <Badge
                variant="outline"
                className={cn(
                  "shrink-0",
                  e.status === "error"
                    ? "bg-destructive/15 text-destructive"
                    : STAGE_COLOR[e.stage],
                )}
              >
                {STAGE_LABEL[e.stage]}
              </Badge>
              {e.chapterIndex !== undefined && (
                <span className="text-muted-foreground">
                  CH{String(e.chapterIndex + 1).padStart(2, "0")}
                </span>
              )}
              {e.attempt > 0 && (
                <span className="text-accent">retry #{e.attempt}</span>
              )}
            </div>
            <span className="text-muted-foreground tabular-nums">
              {formatDuration(e.durationMs)}
            </span>
          </div>
          <div className="mt-1 flex items-center justify-between gap-2 text-muted-foreground">
            <span className="truncate">{formatTime(e.startedAt)}</span>
            <span className="tabular-nums shrink-0">
              {e.totalTokens > 0
                ? `${e.promptTokens}↗${e.completionTokens} = ${e.totalTokens}`
                : "—"}
            </span>
          </div>
          {e.status === "error" && e.errorMessage && (
            <p className="mt-1 text-destructive break-words whitespace-pre-wrap">
              {e.errorStatus ? `[${e.errorStatus}] ` : ""}
              {e.errorMessage}
            </p>
          )}
        </li>
      ))}
    </ul>
  );
}

function ErrorsTab({ events }: { events: LlmCallEvent[] }) {
  const errors = useMemo(
    () => events.filter((e) => e.status === "error").reverse(),
    [events],
  );
  if (errors.length === 0) {
    return (
      <div className="rounded-xl border border-success/30 bg-success/5 p-6 text-center">
        <CheckCircle2 className="size-5 mx-auto text-success" />
        <p className="mt-2 text-[13px] text-success">无错误记录</p>
      </div>
    );
  }
  return (
    <ul className="space-y-2">
      {errors.map((e) => (
        <li
          key={e.id}
          className="rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2"
        >
          <div className="flex items-center justify-between gap-2 text-[11px] font-mono">
            <div className="flex items-center gap-2">
              <AlertCircle className="size-3.5 text-destructive" />
              <span className="font-semibold">{STAGE_LABEL[e.stage]}</span>
              {e.chapterIndex !== undefined && (
                <span className="text-muted-foreground">
                  CH {String(e.chapterIndex + 1).padStart(2, "0")}
                </span>
              )}
              {e.attempt > 0 && (
                <span className="text-accent">retry #{e.attempt}</span>
              )}
            </div>
            <span className="text-muted-foreground tabular-nums">
              {formatTime(e.startedAt)}
            </span>
          </div>
          {e.blockIds && e.blockIds.length > 0 && (
            <p className="mt-1 text-[11px] text-muted-foreground font-mono">
              blocks: {e.blockIds.join(", ")}
            </p>
          )}
          <p className="mt-1 text-[12px] break-words whitespace-pre-wrap">
            {e.errorStatus ? (
              <span className="font-mono text-destructive mr-1">
                [{e.errorStatus}]
              </span>
            ) : null}
            {e.errorMessage}
          </p>
        </li>
      ))}
    </ul>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section>
      <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-2">
        {title}
      </p>
      <div>{children}</div>
    </section>
  );
}

function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3_600_000) return `${(ms / 60_000).toFixed(1)}m`;
  return `${(ms / 3_600_000).toFixed(1)}h`;
}

function formatTime(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const now = Date.now();
  const diff = now - d.getTime();
  if (diff < 60_000) return `${Math.max(1, Math.round(diff / 1000))}s 前`;
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m 前`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h 前`;
  return d.toLocaleString();
}

export function TranslationStatusButton({
  storyID,
  className,
  label,
}: {
  storyID: string;
  className?: string;
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        className={cn("gap-1", className)}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setOpen(true);
        }}
        aria-label="翻译状态"
      >
        <Activity className="size-3.5" />
        {label ?? "状态"}
      </Button>
      <TranslationStatusPanel
        storyID={storyID}
        open={open}
        onClose={() => setOpen(false)}
      />
    </>
  );
}
