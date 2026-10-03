import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Activity,
  AlertCircle,
  CheckCircle2,
  ChevronDown,
  Clock,
  Cpu,
  FileText,
  RefreshCw,
  Sparkles,
  Trash2,
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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import { api, subscribeStream } from "../lib/api";
import { useAuth } from "../lib/auth";
import { STAGE_LABEL } from "../lib/status";

type Props = {
  storyID: string;
  /** Shown under the sheet title; falls back to the story ID. */
  title?: string;
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
  title,
  open,
  onClose,
  returnFocusRef,
}: Props) {
  const qc = useQueryClient();
  const { user } = useAuth();
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [confirmAction, setConfirmAction] = useState<"reset" | "reanalyze" | null>(
    null,
  );

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
    onSuccess: () => {
      toast.success("翻译统计已重置");
      return refetch();
    },
    onError: (resetError) =>
      toast.error("重置失败", { description: resetError.message }),
  });

  const reanalyze = useMutation({
    mutationFn: () => api.reanalyze(storyID),
    onSuccess: () => {
      toast.success("已重新入队", { description: "将重新预读并翻译全文。" });
      refetch();
      qc.invalidateQueries({ queryKey: ["stories"] });
    },
    onError: (reanalyzeError) =>
      toast.error("操作失败", { description: reanalyzeError.message }),
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
          onCloseAutoFocus={(event) => {
            if (!returnFocusRef?.current) return;
            event.preventDefault();
            returnFocusRef.current.focus();
          }}
          className="w-full gap-0 overflow-y-auto sm:max-w-2xl"
        >
          <SheetHeader className="sticky top-0 z-10 gap-2 border-b bg-popover/95 pr-14 backdrop-blur-md">
            <div className="flex items-center gap-2">
              <Activity className="size-4 shrink-0 text-primary" />
              <SheetTitle>翻译状态</SheetTitle>
              {data?.mode && (
                <Badge variant={data.mode === "refined" ? "accent" : "secondary"}>
                  {data.mode === "refined" ? "精翻" : "普通"}
                </Badge>
              )}
              <Button
                variant="ghost"
                size="sm"
                className="ml-auto"
                onClick={() => setAutoRefresh((v) => !v)}
                aria-pressed={autoRefresh}
                title={autoRefresh ? "暂停自动刷新" : "恢复自动刷新"}
              >
                <RefreshCw
                  data-icon="inline-start"
                  className={cn(autoRefresh && isFetching && "animate-spin")}
                />
                {autoRefresh ? "实时" : "已暂停"}
              </Button>
            </div>
            <SheetDescription
              className={cn("truncate", !title && "font-mono text-xs")}
            >
              {title || storyID}
            </SheetDescription>
          </SheetHeader>

          <div className="flex flex-col gap-4 p-4">
            {isLoading && <Skeleton className="h-40 w-full" />}
            {error && (
              <Alert variant="destructive">
                <AlertCircle />
                <AlertTitle>状态加载失败</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
              </Alert>
            )}
            {data && (
              <Tabs defaultValue="overview">
                <TabsList className="w-full">
                  <TabsTrigger value="overview">概览</TabsTrigger>
                  <TabsTrigger value="context">预读</TabsTrigger>
                  <TabsTrigger value="samples">请求</TabsTrigger>
                  <TabsTrigger value="events">调用</TabsTrigger>
                  <TabsTrigger value="errors">
                    错误
                    {data.events.some((e) => e.status === "error") && (
                      <Badge variant="destructive">
                        {data.events.filter((e) => e.status === "error").length}
                      </Badge>
                    )}
                  </TabsTrigger>
                </TabsList>

                <TabsContent value="overview" className="mt-4">
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

                <TabsContent value="context" className="mt-4">
                  <ContextTab data={data} />
                </TabsContent>

                <TabsContent value="samples" className="mt-4">
                  <SamplesTab samples={data.samples} canSeeRaw={!!user} />
                </TabsContent>

                <TabsContent value="events" className="mt-4">
                  <EventsTab events={data.events} />
                </TabsContent>

                <TabsContent value="errors" className="mt-4">
                  <ErrorsTab events={data.events} />
                </TabsContent>
              </Tabs>
            )}
          </div>
        </SheetContent>
      </Sheet>

      <AlertDialog
        open={!!confirmAction}
        onOpenChange={(next) => !next && setConfirmAction(null)}
      >
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
              {confirmAction === "reset" ? "确认重置" : "重新预读"}
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
  const stages = Object.entries(data.stats.byStage) as [
    LlmCallStage,
    StageStats,
  ][];

  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-2">
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
          sub={`均 ${avgTokens.toLocaleString()} / 次`}
        />
        <StatCard
          icon={CheckCircle2}
          label="成功 / 失败"
          value={`${total.successes} / ${total.failures}`}
          sub={`重试 ${total.retries}`}
        />
        <StatCard
          icon={Clock}
          label="平均耗时"
          value={formatDuration(avgDuration)}
          sub={`累计 ${formatDuration(total.durationMs)}`}
        />
      </div>

      <Section title="按阶段">
        {stages.length === 0 ? (
          <p className="text-xs text-muted-foreground">暂无调用记录</p>
        ) : (
          <div className="flex flex-col gap-2">
            {stages
              .sort((a, b) => b[1].calls - a[1].calls)
              .map(([stage, stats]) => (
                <StageRow key={stage} stage={stage} stats={stats} />
              ))}
          </div>
        )}
      </Section>

      {(data.stats.startedAt || data.stats.lastCallAt) && (
        <p className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {data.stats.startedAt && <span>首次 {formatTime(data.stats.startedAt)}</span>}
          {data.stats.startedAt && data.stats.lastCallAt && <span>·</span>}
          {data.stats.lastCallAt && <span>最近 {formatTime(data.stats.lastCallAt)}</span>}
        </p>
      )}

      {canManage && (
        <>
          <Separator />
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={actionPending}
              onClick={onReanalyze}
            >
              {reanalyzing ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <Sparkles data-icon="inline-start" />
              )}
              {reanalyzing ? "入队中" : "重新预读并翻译"}
            </Button>
            <Button
              variant="destructive"
              size="sm"
              disabled={actionPending}
              onClick={onReset}
            >
              {resetting ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <Trash2 data-icon="inline-start" />
              )}
              {resetting ? "重置中" : "重置统计"}
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
    <div className="flex flex-col gap-1 rounded-lg border bg-card p-3">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5 text-primary" />
        {label}
      </div>
      <p className="text-lg font-semibold tabular-nums">{value}</p>
      {sub && <p className="text-xs text-muted-foreground">{sub}</p>}
    </div>
  );
}

function StageRow({ stage, stats }: { stage: LlmCallStage; stats: StageStats }) {
  const successRate =
    stats.calls > 0 ? Math.round((stats.successes / stats.calls) * 100) : 0;
  return (
    <div className="rounded-lg border px-3 py-2 text-xs">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Badge variant={STAGE_VARIANT[stage]}>{STAGE_LABEL[stage]}</Badge>
        <span className="tabular-nums text-muted-foreground">
          {stats.calls} 次 · 成功率 {successRate}% ·{" "}
          {stats.totalTokens.toLocaleString()} token
        </span>
      </div>
      {(stats.failures > 0 || stats.retries > 0) && (
        <p className="mt-1 flex gap-3 text-muted-foreground">
          {stats.failures > 0 && (
            <span className="text-destructive">失败 {stats.failures}</span>
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
            : "当前为普通模式，不会生成预读上下文"
        }
      />
    );
  }
  return (
    <div className="flex flex-col gap-4">
      {ctx.summary && (
        <Section title="全文摘要">
          <p className="text-sm leading-relaxed whitespace-pre-wrap">
            {ctx.summary}
          </p>
        </Section>
      )}
      {ctx.tone && (
        <Section title="风格基调">
          <p className="text-sm leading-relaxed">{ctx.tone}</p>
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
              <li key={c.name} className="rounded-lg border px-3 py-2 text-xs">
                <p className="font-medium">
                  {c.name}
                  {c.zh && (
                    <span className="ml-2 font-normal text-muted-foreground">
                      · {c.zh}
                    </span>
                  )}
                </p>
                {c.role && (
                  <p className="mt-0.5 text-muted-foreground">{c.role}</p>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}
      {Object.keys(ctx.glossary).length > 0 && (
        <Section title={`术语表 (${Object.keys(ctx.glossary).length})`}>
          <div className="flex flex-col gap-1 font-mono text-xs">
            {Object.entries(ctx.glossary).map(([k, v]) => (
              <div
                key={k}
                className="flex items-center justify-between gap-3 rounded-md border px-2 py-1"
              >
                <span className="truncate">{k}</span>
                <span className="truncate text-muted-foreground">{v}</span>
              </div>
            ))}
          </div>
        </Section>
      )}
      {ctx.chapterSummaries.length > 0 && (
        <Section title={`分章摘要 (${ctx.chapterSummaries.length})`}>
          <ol className="flex flex-col gap-2">
            {ctx.chapterSummaries.map((c) => (
              <li key={c.index} className="rounded-lg border px-3 py-2 text-xs">
                <div className="flex items-baseline gap-2">
                  <span className="shrink-0 tabular-nums text-muted-foreground">
                    {chapterLabel(c.index)}
                  </span>
                  {c.title && <span className="truncate font-medium">{c.title}</span>}
                </div>
                <p className="mt-1 leading-relaxed">{c.summary}</p>
              </li>
            ))}
          </ol>
        </Section>
      )}
      <p className="text-xs text-muted-foreground">
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
    <div className="overflow-hidden rounded-lg border">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/50 px-3 py-2 text-xs">
        <div className="flex items-center gap-2">
          <Badge variant={STAGE_VARIANT[stage]}>{STAGE_LABEL[stage]}</Badge>
          {sample.chapterIndex !== undefined && (
            <span className="tabular-nums text-muted-foreground">
              {chapterLabel(sample.chapterIndex)}
            </span>
          )}
          {sample.blockIds && sample.blockIds.length > 0 && (
            <span className="tabular-nums text-muted-foreground">
              {sample.blockIds.length} 段
            </span>
          )}
        </div>
        <span className="text-muted-foreground">
          {formatTime(sample.capturedAt)}
        </span>
      </div>
      <div className="flex flex-col gap-3 px-3 py-3">
        <details
          open={showSystem}
          onToggle={(e) =>
            setShowSystem((e.currentTarget as HTMLDetailsElement).open)
          }
        >
          <summary className="flex cursor-pointer items-center justify-between text-xs font-medium tracking-wide text-muted-foreground uppercase">
            <span>系统提示词</span>
            <ChevronDown
              className={cn("size-3 transition-transform", showSystem && "rotate-180")}
            />
          </summary>
          <Pre className="mt-2 max-h-[200px]">{sample.systemPrompt || "(空)"}</Pre>
        </details>

        <div>
          <PreLabel>请求内容</PreLabel>
          <Pre className="max-h-[280px]">{sample.userPayload || "(空)"}</Pre>
        </div>

        {sample.responsePreview && (
          <div>
            <PreLabel>响应预览</PreLabel>
            <Pre className="max-h-[200px]">{sample.responsePreview}</Pre>
          </div>
        )}
      </div>
    </div>
  );
}

function PreLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="mb-1.5 text-xs font-medium tracking-wide text-muted-foreground uppercase">
      {children}
    </p>
  );
}

function Pre({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <pre
      className={cn(
        "overflow-auto rounded-md bg-muted p-2 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap",
        className,
      )}
    >
      {children}
    </pre>
  );
}

function EventsTab({ events }: { events: LlmCallEvent[] }) {
  const recent = useMemo(() => [...events].reverse(), [events]);
  if (recent.length === 0) {
    return (
      <PanelEmpty
        icon={Activity}
        title="尚无调用记录"
        description="新的模型调用会显示在这里。"
      />
    );
  }
  return (
    <ul className="flex flex-col gap-1.5">
      {recent.map((e) => (
        <li
          key={e.id}
          className={cn(
            "rounded-lg border px-3 py-2 font-mono text-xs",
            e.status === "error" && "border-destructive/40 bg-destructive/5",
          )}
        >
          <div className="flex items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-2">
              <Badge
                variant={
                  e.status === "error" ? "destructive" : STAGE_VARIANT[e.stage]
                }
              >
                {STAGE_LABEL[e.stage]}
              </Badge>
              {e.chapterIndex !== undefined && (
                <span className="text-muted-foreground">
                  {chapterLabel(e.chapterIndex)}
                </span>
              )}
              {e.attempt > 0 && (
                <span className="text-muted-foreground">
                  重试 #{e.attempt}
                </span>
              )}
            </div>
            <span className="tabular-nums text-muted-foreground">
              {formatDuration(e.durationMs)}
            </span>
          </div>
          <div className="mt-1 flex items-center justify-between gap-2 text-muted-foreground">
            <span className="truncate">{formatTime(e.startedAt)}</span>
            <span className="shrink-0 tabular-nums">
              {e.totalTokens > 0
                ? `${e.promptTokens}↗${e.completionTokens} = ${e.totalTokens}`
                : "—"}
            </span>
          </div>
          {e.status === "error" && e.errorMessage && (
            <p className="mt-1 break-words whitespace-pre-wrap text-destructive">
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
      <PanelEmpty
        icon={CheckCircle2}
        title="没有错误记录"
        description="最近的翻译调用均未出错。"
      />
    );
  }
  return (
    <ul className="flex flex-col gap-2">
      {errors.map((e) => (
        <li
          key={e.id}
          className="rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2"
        >
          <div className="flex items-center justify-between gap-2 font-mono text-xs">
            <div className="flex items-center gap-2">
              <AlertCircle className="size-3.5 text-destructive" />
              <span className="font-medium">{STAGE_LABEL[e.stage]}</span>
              {e.chapterIndex !== undefined && (
                <span className="text-muted-foreground">
                  {chapterLabel(e.chapterIndex)}
                </span>
              )}
              {e.attempt > 0 && (
                <span className="text-muted-foreground">
                  重试 #{e.attempt}
                </span>
              )}
            </div>
            <span className="tabular-nums text-muted-foreground">
              {formatTime(e.startedAt)}
            </span>
          </div>
          {e.blockIds && e.blockIds.length > 0 && (
            <p className="mt-1 font-mono text-xs text-muted-foreground">
              段落：{e.blockIds.join(", ")}
            </p>
          )}
          <p className="mt-1 text-sm break-words whitespace-pre-wrap">
            {e.errorStatus ? (
              <span className="mr-1 font-mono text-destructive">
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
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
        {title}
      </h3>
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
    <Empty className="min-h-44 border border-dashed">
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

function chapterLabel(index: number): string {
  return `第 ${index + 1} 章`;
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
  const diff = Date.now() - d.getTime();
  if (diff < 60_000) return `${Math.max(1, Math.round(diff / 1000))}s 前`;
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m 前`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h 前`;
  return d.toLocaleString();
}

export function TranslationStatusButton({
  storyID,
  title,
  className,
  label,
}: {
  storyID: string;
  title?: string;
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
        className={className}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setOpen(true);
        }}
      >
        <Activity data-icon="inline-start" />
        {label ?? "状态"}
      </Button>
      <TranslationStatusPanel
        storyID={storyID}
        title={title}
        open={open}
        onClose={() => setOpen(false)}
        returnFocusRef={triggerRef}
      />
    </>
  );
}
