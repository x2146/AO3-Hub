import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
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
  type LucideIcon,
} from "lucide-react";
import type {
  LlmCallEvent,
  LlmCallStage,
  RequestSample,
  StageStats,
  TranslationStatusView,
} from "@ao3hub/shared";
import { Badge } from "@/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import { api, subscribeStream } from "../lib/api";
import { useAuth } from "../lib/auth";
import { STAGE_LABEL } from "../lib/status";

type Props = {
  storyID: string;
  open: boolean;
  onClose: () => void;
  returnFocusRef?: RefObject<HTMLButtonElement>;
};

const STAGE_VARIANT: Record<LlmCallStage, "accent" | "success"> = {
  "analysis-chapter": "accent",
  "analysis-merge": "accent",
  "analysis-full": "accent",
  "translate-batch": "success",
};

export function TranslationStatusPanel({
  storyID,
  open,
  onClose,
  returnFocusRef,
}: Props) {
  const qc = useQueryClient();
  const { user } = useAuth();
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [confirmAction, setConfirmAction] = useState<"reset" | "reanalyze" | null>(null);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["translation-status", storyID],
    queryFn: ({ signal }) => api.getTranslationStatus(storyID, signal),
    enabled: open,
    refetchInterval: autoRefresh && open ? 2500 : false,
  });

  useEffect(() => {
    if (!open || !autoRefresh) return;
    const unsub = subscribeStream(storyID, (event) => {
      if (event.type === "llm-call" || event.type === "phase") {
        qc.invalidateQueries({ queryKey: ["translation-status", storyID] });
      }
    });
    return unsub;
  }, [open, autoRefresh, storyID, qc]);

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
  const actionPending = resetStats.isPending || reanalyze.isPending;

  useEffect(() => {
    if (open) return;
    resetStats.reset();
    reanalyze.reset();
  }, [open]);

  return (
    <>
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent
        side="right"
        showCloseButton={false}
        onCloseAutoFocus={(event) => {
          if (!returnFocusRef?.current) return;
          event.preventDefault();
          returnFocusRef.current.focus();
        }}
        className="w-full gap-0 overflow-y-auto p-0 sm:max-w-2xl"
      >
        <header className="sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-border bg-card/95 backdrop-blur px-5 py-3">
          <div className="flex items-center gap-2 min-w-0">
            <Activity className="shrink-0 text-primary" />
            <div className="min-w-0">
              <SheetTitle>
                翻译状态
              </SheetTitle>
              <SheetDescription className="truncate">
                <code>{storyID}</code>
              </SheetDescription>
              <p className="text-[11px] text-muted-foreground font-mono truncate">
                {data?.mode === "refined" && (
                  <Badge variant="accent">refined</Badge>
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
                data-icon="inline-start"
                className={cn(autoRefresh && isFetching && "animate-spin")}
              />
              {autoRefresh ? "Live" : "Paused"}
            </Button>
            <SheetClose asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="关闭"
              >
                <X data-icon="inline-start" />
              </Button>
            </SheetClose>
          </div>
        </header>

        <div className="px-5 py-4">
          {isLoading && <Skeleton className="h-40 w-full" />}
          {error && (
            <Alert variant="destructive">
              <AlertCircle />
              <AlertTitle>状态加载失败</AlertTitle>
              <AlertDescription>{error.message}</AlertDescription>
            </Alert>
          )}
          {(resetStats.isError || reanalyze.isError) && (
            <Alert variant="destructive" className="mb-3">
              <AlertCircle />
              <AlertTitle>操作失败</AlertTitle>
              <AlertDescription>
                {(resetStats.error ?? reanalyze.error)?.message ?? "未知错误"}
              </AlertDescription>
            </Alert>
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
                  actionPending={actionPending}
                  onReset={() => setConfirmAction("reset")}
                  resetting={resetStats.isPending}
                  onReanalyze={() => setConfirmAction("reanalyze")}
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
      </SheetContent>
    </Sheet>
    <AlertDialog open={!!confirmAction} onOpenChange={(next) => !next && setConfirmAction(null)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            {confirmAction === "reset" ? <Trash2 /> : <Sparkles />}
          </AlertDialogMedia>
          <AlertDialogTitle>
            {confirmAction === "reset" ? "重置翻译统计？" : "重新预读并翻译？"}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {confirmAction === "reset"
              ? "这会清除该作品的全部翻译调用统计，但不会删除现有译文。"
              : "这会清空已生成的上下文，并以精翻模式重新进入任务队列。"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            variant={confirmAction === "reset" ? "destructive" : "default"}
            onClick={() => {
              if (confirmAction === "reset") resetStats.mutate();
              if (confirmAction === "reanalyze") reanalyze.mutate();
              setConfirmAction(null);
            }}
          >
            确认
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
    </>
  );
}

function OverviewTab({
  data,
  canManage,
  actionPending,
  onReset,
  resetting,
  onReanalyze,
  reanalyzing,
}: {
  data: TranslationStatusView;
  canManage: boolean;
  actionPending: boolean;
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
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3">
        <StatCard
          icon={Cpu}
          label="API 调用"
          value={total.calls.toLocaleString()}
          sub={`成功率 ${successRate}%`}
        />
        <StatCard
          icon={Zap}
          label="总 Token"
          value={total.totalTokens.toLocaleString()}
          sub={`入 ${total.promptTokens.toLocaleString()} · 出 ${total.completionTokens.toLocaleString()}`}
        />
        <StatCard
          icon={CheckCircle2}
          label="成功 / 失败"
          value={`${total.successes} / ${total.failures}`}
          sub={`重试 ${total.retries}`}
        />
        <StatCard
          icon={Clock}
          label="均时 / 总时"
          value={formatDuration(avgDuration)}
          sub={`累计 ${formatDuration(total.durationMs)}`}
        />
      </div>

      <div>
        <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-2">
          按阶段
        </p>
        <div className="flex flex-col gap-2">
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
              disabled={actionPending}
              onClick={onReanalyze}
            >
              <Sparkles data-icon="inline-start" />
              {reanalyzing ? "重新入队…" : "重新预读 + 翻译"}
            </Button>
            <Button
              variant="destructive"
              size="sm"
              disabled={actionPending}
              onClick={onReset}
            >
              <Trash2 data-icon="inline-start" />
              {resetting ? "重置中…" : "重置统计"}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  sub,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <Card className="gap-3 py-4">
      <CardHeader className="px-4">
        <CardTitle>{value}</CardTitle>
        <CardDescription>{label}</CardDescription>
        <CardAction>
          <Icon className="text-primary" />
        </CardAction>
      </CardHeader>
      {sub && (
        <CardContent className="px-4">
          <CardDescription>{sub}</CardDescription>
        </CardContent>
      )}
    </Card>
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
    <div className="rounded-control border border-border px-3 py-2 text-[12px]">
      <div className="flex items-center justify-between">
        <Badge variant={STAGE_VARIANT[stage]}>
          {STAGE_LABEL[stage]}
        </Badge>
        <span className="text-muted-foreground tabular-nums font-mono">
          {stats.calls} 次 · {successRate}% ·{" "}
          {stats.totalTokens.toLocaleString()} tok
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
      <PanelEmpty
        icon={FileText}
        title="暂无预读分析"
        description={
          data.mode === "refined"
            ? "预读分析尚未生成"
            : "当前为快翻模式，不会生成预读上下文"
        }
      />
    );
  }
  return (
    <div className="flex flex-col gap-4">
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
              <Badge key={s} variant="accent">
                {s}
              </Badge>
            ))}
          </div>
        </Section>
      )}
      {ctx.characters.length > 0 && (
        <Section title={`角色 (${ctx.characters.length})`}>
          <ul className="flex flex-col gap-1.5">
            {ctx.characters.map((c) => (
              <li
                key={c.name}
                className="rounded-control border border-border px-3 py-2 text-[12px]"
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
          <ol className="flex flex-col gap-2">
            {ctx.chapterSummaries.map((c) => (
              <li
                key={c.index}
                className="rounded-control border border-border px-3 py-2 text-[12px]"
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
      <PanelEmpty
        icon={FileText}
        title="尚无请求样本"
        description="开始翻译后会捕获每个阶段的最新一次请求。"
      />
    );
  }
  if (!canSeeRaw) {
    return (
      <PanelEmpty
        icon={FileText}
        title="内容不可见"
        description="请求上下文仅登录用户可见。"
      />
    );
  }
  return (
    <div className="flex flex-col gap-3">
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
    <div className="rounded-control border border-border overflow-hidden">
      <div className="flex items-center justify-between bg-secondary/40 px-3 py-2 text-[12px]">
        <div className="flex items-center gap-2">
          <Badge variant={STAGE_VARIANT[stage]}>
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
      <div className="flex flex-col gap-3 px-3 py-3">
        <details
          className="group"
          open={showSystem}
          onToggle={(e) =>
            setShowSystem((e.currentTarget as HTMLDetailsElement).open)
          }
        >
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
      <PanelEmpty icon={Activity} title="尚无调用记录" description="新的模型调用会显示在这里。" />
    );
  }
  return (
    <ul className="flex flex-col gap-1">
      {recent.map((e) => (
        <li
          key={e.id}
          className={cn(
            "rounded-control border px-3 py-2 text-[11px] font-mono",
            e.status === "error"
              ? "border-destructive/40 bg-destructive/5"
              : "border-border",
          )}
        >
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <Badge
                variant={e.status === "error" ? "destructive" : STAGE_VARIANT[e.stage]}
                className="shrink-0"
              >
                {STAGE_LABEL[e.stage]}
              </Badge>
              {e.chapterIndex !== undefined && (
                <span className="text-muted-foreground">
                  CH{String(e.chapterIndex + 1).padStart(2, "0")}
                </span>
              )}
              {e.attempt > 0 && (
                <span className="text-primary">retry #{e.attempt}</span>
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
      <Alert>
        <CheckCircle2 />
        <AlertTitle>无错误记录</AlertTitle>
        <AlertDescription>最近的翻译调用均未记录错误。</AlertDescription>
      </Alert>
    );
  }
  return (
    <ul className="flex flex-col gap-2">
      {errors.map((e) => (
        <li
          key={e.id}
          className="rounded-control border border-destructive/40 bg-destructive/5 px-3 py-2"
        >
          <div className="flex items-center justify-between gap-2 text-[11px] font-mono">
            <div className="flex items-center gap-2">
              <AlertCircle />
              <span className="font-semibold">{STAGE_LABEL[e.stage]}</span>
              {e.chapterIndex !== undefined && (
                <span className="text-muted-foreground">
                  CH {String(e.chapterIndex + 1).padStart(2, "0")}
                </span>
              )}
              {e.attempt > 0 && (
                <span className="text-primary">retry #{e.attempt}</span>
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

function PanelEmpty({
  icon: Icon,
  title,
  description,
}: {
  icon: LucideIcon;
  title: string;
  description: string;
}) {
  return (
    <Empty className="min-h-44">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Icon />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
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
  const triggerRef = useRef<HTMLButtonElement>(null);
  return (
    <>
      <Button
        ref={triggerRef}
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
        <Activity data-icon="inline-start" />
        {label ?? "状态"}
      </Button>
      <TranslationStatusPanel
        storyID={storyID}
        open={open}
        onClose={() => setOpen(false)}
        returnFocusRef={triggerRef}
      />
    </>
  );
}
