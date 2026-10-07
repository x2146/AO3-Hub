import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Activity,
  AlertCircle,
  CheckCircle2,
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
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Toggle } from "@/components/ui/toggle";
import { cn } from "@/lib/utils";
import { api, subscribeStream } from "../lib/api";
import { useAuth } from "../lib/auth";
import { STAGE_LABEL } from "../lib/status";

type Props = {
  storyID: string;
  /** Shown under the dialog title; falls back to the story ID. */
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

  const errorCount = data?.events.filter((e) => e.status === "error").length ?? 0;

  return (
    <>
      <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
        <DialogContent
          onCloseAutoFocus={(event) => {
            if (!returnFocusRef?.current) return;
            event.preventDefault();
            returnFocusRef.current.focus();
          }}
          className="flex h-[min(46rem,calc(100svh-2rem))] flex-col gap-0 p-0 sm:max-w-3xl"
        >
          <DialogHeader className="border-b p-4 pr-12">
            <div className="flex items-center gap-2">
              <DialogTitle>翻译状态</DialogTitle>
              {data?.mode && (
                <Badge variant={data.mode === "refined" ? "accent" : "secondary"}>
                  {data.mode === "refined" ? "精翻" : "普通"}
                </Badge>
              )}
              <Toggle
                variant="outline"
                size="sm"
                className="ml-auto"
                pressed={autoRefresh}
                onPressedChange={setAutoRefresh}
              >
                <RefreshCw
                  data-icon="inline-start"
                  className={cn(autoRefresh && isFetching && "animate-spin")}
                />
                {autoRefresh ? "实时" : "已暂停"}
              </Toggle>
            </div>
            <DialogDescription className={cn("truncate", !title && "font-mono")}>
              {title || storyID}
            </DialogDescription>
          </DialogHeader>

          {data ? (
            <Tabs defaultValue="overview" className="min-h-0 flex-1 gap-0">
              <div className="px-4 pt-4">
                <TabsList className="w-full">
                  <TabsTrigger value="overview">概览</TabsTrigger>
                  <TabsTrigger value="context">预读</TabsTrigger>
                  <TabsTrigger value="samples">请求</TabsTrigger>
                  <TabsTrigger value="events">调用</TabsTrigger>
                  <TabsTrigger value="errors">
                    错误
                    {errorCount > 0 && (
                      <Badge variant="destructive">{errorCount}</Badge>
                    )}
                  </TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="overview" className="min-h-0 overflow-y-auto p-4">
                <OverviewTab data={data} />
              </TabsContent>

              <TabsContent value="context" className="min-h-0 overflow-y-auto p-4">
                <ContextTab data={data} />
              </TabsContent>

              <TabsContent value="samples" className="min-h-0 overflow-y-auto p-4">
                <SamplesTab samples={data.samples} canSeeRaw={!!user} />
              </TabsContent>

              <TabsContent value="events" className="min-h-0 overflow-y-auto p-4">
                <EventsTab events={data.events} />
              </TabsContent>

              <TabsContent value="errors" className="min-h-0 overflow-y-auto p-4">
                <ErrorsTab events={data.events} />
              </TabsContent>
            </Tabs>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col gap-4 p-4">
              {isLoading && (
                <>
                  <Skeleton className="h-8 w-full" />
                  <div className="grid grid-cols-2 gap-4">
                    <Skeleton className="h-24" />
                    <Skeleton className="h-24" />
                    <Skeleton className="h-24" />
                    <Skeleton className="h-24" />
                  </div>
                </>
              )}
              {error && (
                <Alert variant="destructive">
                  <AlertCircle />
                  <AlertTitle>状态加载失败</AlertTitle>
                  <AlertDescription>{error.message}</AlertDescription>
                </Alert>
              )}
            </div>
          )}

          {data && user && (
            <DialogFooter className="m-0">
              <Button
                variant="destructive"
                disabled={actionPending}
                onClick={() => setConfirmAction("reset")}
              >
                {resetStats.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <Trash2 data-icon="inline-start" />
                )}
                {resetStats.isPending ? "重置中" : "重置统计"}
              </Button>
              <Button
                variant="outline"
                disabled={actionPending}
                onClick={() => setConfirmAction("reanalyze")}
              >
                {reanalyze.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <Sparkles data-icon="inline-start" />
                )}
                {reanalyze.isPending ? "入队中" : "重新预读并翻译"}
              </Button>
            </DialogFooter>
          )}
        </DialogContent>
      </Dialog>

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

function OverviewTab({ data }: { data: TranslationStatusView }) {
  const total = data.stats.total;
  const successRate = rate(total.successes, total.calls);
  const avgTokens =
    total.calls > 0 ? Math.round(total.totalTokens / total.calls) : 0;
  const avgDuration =
    total.calls > 0 ? Math.round(total.durationMs / total.calls) : 0;
  const stages = (
    Object.entries(data.stats.byStage) as [LlmCallStage, StageStats][]
  ).sort((a, b) => b[1].calls - a[1].calls);
  const timeline = [
    data.stats.startedAt && `首次 ${formatTime(data.stats.startedAt)}`,
    data.stats.lastCallAt && `最近 ${formatTime(data.stats.lastCallAt)}`,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-4">
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

      <Card size="sm">
        <CardHeader>
          <CardTitle>按阶段</CardTitle>
          {timeline && <CardDescription>{timeline}</CardDescription>}
        </CardHeader>
        <CardContent>
          {stages.length === 0 ? (
            <p className="text-muted-foreground">暂无调用记录</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>阶段</TableHead>
                  <TableHead className="text-right">调用</TableHead>
                  <TableHead className="text-right">成功率</TableHead>
                  <TableHead className="text-right">失败</TableHead>
                  <TableHead className="hidden text-right sm:table-cell">重试</TableHead>
                  <TableHead className="text-right">Token</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {stages.map(([stage, stats]) => (
                  <TableRow key={stage}>
                    <TableCell>
                      <Badge variant={STAGE_VARIANT[stage]}>
                        {STAGE_LABEL[stage]}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {stats.calls}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {rate(stats.successes, stats.calls)}%
                    </TableCell>
                    <TableCell
                      className={cn(
                        "text-right tabular-nums",
                        stats.failures > 0 && "text-destructive",
                      )}
                    >
                      {stats.failures}
                    </TableCell>
                    <TableCell className="hidden text-right tabular-nums sm:table-cell">
                      {stats.retries}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {stats.totalTokens.toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
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
  sub: string;
}) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardDescription>{label}</CardDescription>
        <CardTitle className="text-xl tabular-nums">{value}</CardTitle>
        <CardAction>
          <Icon className="size-4 text-muted-foreground" />
        </CardAction>
      </CardHeader>
      <CardContent className="text-xs text-muted-foreground">{sub}</CardContent>
    </Card>
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
  const glossary = Object.entries(ctx.glossary);
  return (
    <div className="flex flex-col gap-4">
      {ctx.summary && (
        <Card size="sm">
          <CardHeader>
            <CardTitle>全文摘要</CardTitle>
            <CardDescription>
              生成于 {formatTime(ctx.generatedAt)} · {ctx.chapterCount ?? "?"} 章
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="leading-relaxed whitespace-pre-wrap">{ctx.summary}</p>
          </CardContent>
        </Card>
      )}
      {ctx.tone && (
        <Card size="sm">
          <CardHeader>
            <CardTitle>风格基调</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="leading-relaxed">{ctx.tone}</p>
          </CardContent>
        </Card>
      )}
      {ctx.ships.length > 0 && (
        <Card size="sm">
          <CardHeader>
            <CardTitle>Ships</CardTitle>
            <CardAction>
              <Badge variant="secondary">{ctx.ships.length}</Badge>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-1.5">
            {ctx.ships.map((s) => (
              <Badge key={s} variant="accent">
                {s}
              </Badge>
            ))}
          </CardContent>
        </Card>
      )}
      {ctx.characters.length > 0 && (
        <Card size="sm">
          <CardHeader>
            <CardTitle>角色</CardTitle>
            <CardAction>
              <Badge variant="secondary">{ctx.characters.length}</Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-1/4">原名</TableHead>
                  <TableHead className="w-1/4">译名</TableHead>
                  <TableHead>定位</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {ctx.characters.map((c) => (
                  <TableRow key={c.name}>
                    <TableCell className="whitespace-normal font-medium">
                      {c.name}
                    </TableCell>
                    <TableCell className="whitespace-normal">{c.zh || "—"}</TableCell>
                    <TableCell className="whitespace-normal text-muted-foreground">
                      {c.role || "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
      {glossary.length > 0 && (
        <Card size="sm">
          <CardHeader>
            <CardTitle>术语表</CardTitle>
            <CardAction>
              <Badge variant="secondary">{glossary.length}</Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>原文</TableHead>
                  <TableHead>译名</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {glossary.map(([k, v]) => (
                  <TableRow key={k}>
                    <TableCell className="whitespace-normal">{k}</TableCell>
                    <TableCell className="whitespace-normal">{v}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
      {ctx.chapterSummaries.length > 0 && (
        <Card size="sm">
          <CardHeader>
            <CardTitle>分章摘要</CardTitle>
            <CardAction>
              <Badge variant="secondary">{ctx.chapterSummaries.length}</Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <Accordion type="multiple">
              {ctx.chapterSummaries.map((c) => (
                <AccordionItem key={c.index} value={String(c.index)}>
                  <AccordionTrigger>
                    <span className="flex min-w-0 gap-2">
                      <span className="shrink-0 tabular-nums text-muted-foreground">
                        {chapterLabel(c.index)}
                      </span>
                      {c.title && <span className="truncate">{c.title}</span>}
                    </span>
                  </AccordionTrigger>
                  <AccordionContent className="leading-relaxed">
                    {c.summary}
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </CardContent>
        </Card>
      )}
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
    <div className="flex flex-col gap-4">
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
  const meta = [
    sample.chapterIndex !== undefined && chapterLabel(sample.chapterIndex),
    sample.blockIds?.length && `${sample.blockIds.length} 段`,
    formatTime(sample.capturedAt),
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>{STAGE_LABEL[stage]}</CardTitle>
        <CardDescription>{meta}</CardDescription>
        <CardAction>
          <Badge variant={STAGE_VARIANT[stage]}>最新样本</Badge>
        </CardAction>
      </CardHeader>
      <CardContent>
        <Tabs defaultValue="payload">
          <TabsList variant="line">
            <TabsTrigger value="payload">请求内容</TabsTrigger>
            <TabsTrigger value="system">系统提示词</TabsTrigger>
            {sample.responsePreview && (
              <TabsTrigger value="response">响应预览</TabsTrigger>
            )}
          </TabsList>
          <TabsContent value="payload">
            <CodeBlock>{sample.userPayload || "(空)"}</CodeBlock>
          </TabsContent>
          <TabsContent value="system">
            <CodeBlock>{sample.systemPrompt || "(空)"}</CodeBlock>
          </TabsContent>
          {sample.responsePreview && (
            <TabsContent value="response">
              <CodeBlock>{sample.responsePreview}</CodeBlock>
            </TabsContent>
          )}
        </Tabs>
      </CardContent>
    </Card>
  );
}

function CodeBlock({ children }: { children: React.ReactNode }) {
  return (
    <pre className="max-h-72 overflow-auto rounded-lg bg-muted p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">
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
    <Card size="sm">
      <CardHeader>
        <CardTitle>最近调用</CardTitle>
        <CardDescription>按时间倒序，Token 为输入 / 输出</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>阶段</TableHead>
              <TableHead className="hidden sm:table-cell">章节</TableHead>
              <TableHead className="text-right">Token</TableHead>
              <TableHead className="text-right">耗时</TableHead>
              <TableHead className="hidden text-right sm:table-cell">时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {recent.map((e) => (
              <TableRow key={e.id}>
                <TableCell>
                  <div className="flex items-center gap-1.5">
                    <Badge
                      variant={
                        e.status === "error" ? "destructive" : STAGE_VARIANT[e.stage]
                      }
                    >
                      {e.status === "error" && <AlertCircle />}
                      {STAGE_LABEL[e.stage]}
                    </Badge>
                    {e.attempt > 0 && (
                      <Badge variant="outline">重试 #{e.attempt}</Badge>
                    )}
                  </div>
                </TableCell>
                <TableCell className="hidden text-muted-foreground sm:table-cell">
                  {e.chapterIndex !== undefined ? chapterLabel(e.chapterIndex) : "—"}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {e.totalTokens > 0 ? (
                    <>
                      {e.promptTokens.toLocaleString()}
                      <span className="text-muted-foreground"> / </span>
                      {e.completionTokens.toLocaleString()}
                    </>
                  ) : (
                    "—"
                  )}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatDuration(e.durationMs)}
                </TableCell>
                <TableCell className="hidden text-right text-muted-foreground sm:table-cell">
                  {formatTime(e.startedAt)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
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
    <div className="flex flex-col gap-2">
      {errors.map((e) => {
        const heading = [
          STAGE_LABEL[e.stage],
          e.chapterIndex !== undefined && chapterLabel(e.chapterIndex),
          e.attempt > 0 && `重试 #${e.attempt}`,
        ]
          .filter(Boolean)
          .join(" · ");
        return (
          <Alert key={e.id} variant="destructive">
            <AlertCircle />
            <AlertTitle>{heading}</AlertTitle>
            <AlertDescription>
              <p className="whitespace-pre-wrap [overflow-wrap:anywhere]">
                {e.errorStatus ? `[${e.errorStatus}] ` : ""}
                {e.errorMessage}
              </p>
              <p className="text-muted-foreground [overflow-wrap:anywhere]">
                {formatTime(e.startedAt)}
                {e.blockIds && e.blockIds.length > 0 && (
                  <> · {formatBlockIds(e.blockIds)}</>
                )}
              </p>
            </AlertDescription>
          </Alert>
        );
      })}
    </div>
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

function rate(part: number, whole: number): number {
  return whole > 0 ? Math.round((part / whole) * 100) : 0;
}

/** Batches can span dozens of hash ids; the first few are enough to locate it. */
function formatBlockIds(ids: string[]): string {
  const shown = ids.slice(0, 3).join(", ");
  return ids.length > 3 ? `段落 ${shown} 等 ${ids.length} 段` : `段落 ${shown}`;
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
