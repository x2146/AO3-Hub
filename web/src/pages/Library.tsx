import { Link } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUpRight,
  BookOpenText,
  FilePlus2,
  LibraryBig,
  LogIn,
  RotateCcw,
  Sparkles,
  Trash2,
} from "lucide-react";
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
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Separator } from "@/components/ui/separator";
import { api, type StoriesListResponse } from "../lib/api";
import { useAuth } from "../lib/auth";
import { StatusPill } from "../components/StatusPill";
import {
  TranslateProgressBar,
  TranslateProgressLegend,
  breakdownOf,
} from "../components/TranslateProgress";
import { TranslationStatusButton } from "../components/TranslationStatusPanel";
import { isInFlight } from "../lib/status";

type DeleteTarget = { id: string; title: string };

export function Library() {
  const qc = useQueryClient();
  const actionPendingRef = useRef(false);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const { user } = useAuth();
  const { data: config } = useQuery({
    queryKey: ["config", "public"],
    queryFn: ({ signal }) => api.getPublicConfig(signal),
  });
  const { data, isLoading, error } = useQuery({
    queryKey: ["stories"],
    queryFn: ({ signal }) => api.listStories(signal),
    refetchInterval: (q) => {
      const stories = (q.state.data as StoriesListResponse | undefined)?.stories;
      const inFlight = stories?.some((story) => isInFlight(story.status));
      return inFlight ? (config?.ui.libraryRefetchIntervalMs ?? 3000) : false;
    },
  });

  const del = useMutation({
    mutationFn: (id: string) => api.remove(id),
    onSuccess: () => {
      setDeleteTarget(null);
      return qc.invalidateQueries({ queryKey: ["stories"] });
    },
    onSettled: () => {
      actionPendingRef.current = false;
    },
  });

  const retry = useMutation({
    mutationFn: (id: string) => api.retry(id, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["stories"] }),
    onSettled: () => {
      actionPendingRef.current = false;
    },
  });
  const actionPending = del.isPending || retry.isPending;

  const retryStory = (id: string) => {
    if (actionPendingRef.current) return;
    actionPendingRef.current = true;
    retry.mutate(id);
  };

  const deleteStory = (id: string) => {
    if (actionPendingRef.current) return;
    actionPendingRef.current = true;
    del.mutate(id);
  };

  if (isLoading) return <LibrarySkeleton />;

  if (error) {
    return (
      <Alert variant="destructive">
        <LibraryBig />
        <AlertTitle>书架加载失败</AlertTitle>
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    );
  }

  const stories = data?.stories ?? [];
  const readyCount = stories.filter((story) => story.status === "ready").length;
  const inFlightCount = stories.filter((story) => isInFlight(story.status)).length;

  return (
    <div className="flex flex-col gap-8 fade-in">
      <Card className="overflow-hidden">
        <CardHeader className="gap-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
          <div className="flex min-w-0 flex-col gap-3">
            <Badge variant="accent">
              <Sparkles />
              Personal archive
            </Badge>
            <div className="flex flex-col gap-2">
              <CardTitle>
                <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">
                  你的 AO3 阅读书架
                </h1>
              </CardTitle>
              <CardDescription className="max-w-2xl">
                集中管理作品、翻译进度与阅读状态。上传 AO3 HTML，或直接粘贴作品链接开始翻译。
              </CardDescription>
            </div>
          </div>
          <CardAction className="static col-auto row-auto self-end justify-self-start sm:justify-self-end">
            {user ? (
              <Button size="lg" asChild>
                <Link to="/import">
                  <FilePlus2 data-icon="inline-start" />
                  添加作品
                </Link>
              </Button>
            ) : (
              <Button size="lg" variant="outline" asChild>
                <Link to="/login" search={{ redirect: undefined }}>
                  登录以管理书架
                  <ArrowUpRight data-icon="inline-end" />
                </Link>
              </Button>
            )}
          </CardAction>
        </CardHeader>
        <CardContent>
          <div className="grid gap-3 sm:grid-cols-3">
            <Metric label="全部作品" value={stories.length} />
            <Metric label="可阅读" value={readyCount} />
            <Metric label="处理中" value={inFlightCount} />
          </div>
        </CardContent>
      </Card>

      {(del.isError || retry.isError) && (
        <Alert variant="destructive">
          <RotateCcw />
          <AlertTitle>操作失败</AlertTitle>
          <AlertDescription>
            {(del.error ?? retry.error)?.message ?? "未知错误"}
          </AlertDescription>
        </Alert>
      )}

      {stories.length === 0 ? (
        <Empty className="min-h-[360px]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BookOpenText />
            </EmptyMedia>
            <EmptyTitle>书架还是空的</EmptyTitle>
            <EmptyDescription>
              添加第一篇作品后，你可以在这里追踪翻译进度并直接进入沉浸阅读。
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            {user ? (
              <Button asChild>
                <Link to="/import">
                  <FilePlus2 data-icon="inline-start" />
                  添加第一篇作品
                </Link>
              </Button>
            ) : (
              <Button variant="outline" asChild>
                <Link to="/login" search={{ redirect: undefined }}>
                  <LogIn data-icon="inline-start" />
                  登录后导入
                </Link>
              </Button>
            )}
          </EmptyContent>
        </Empty>
      ) : (
        <section aria-labelledby="works-heading" className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-4">
            <div>
              <h2 id="works-heading" className="text-lg font-semibold tracking-tight">
                全部作品
              </h2>
              <p className="text-sm text-muted-foreground">按导入顺序排列</p>
            </div>
            <Badge variant="secondary">{stories.length} works</Badge>
          </div>
          <ul className="grid gap-4 lg:grid-cols-2">
            {stories.map((story, index) => {
              const showProgress =
                story.progress &&
                (story.status !== "ready" ||
                  (story.progress.totalBlocks ?? 0) > (story.progress.doneBlocks ?? 0));
              const { error: errorCount } = breakdownOf(story.progress);
              const canRetry = !!user && (story.status === "error" || errorCount > 0);
              const retrying = retry.isPending && retry.variables === story.id;

              return (
                <li key={story.id} className="min-w-0">
                  <Card className="h-full transition-transform hover:-translate-y-0.5">
                    <CardHeader>
                      <div className="flex min-w-0 flex-col gap-2 pr-20">
                        <Badge variant="outline">#{String(index + 1).padStart(2, "0")}</Badge>
                        <CardTitle>
                          <Link
                            to="/r/$id/$chapter"
                            params={{ id: story.id, chapter: "0" }}
                            className="line-clamp-2 hover:text-primary"
                          >
                            {story.title}
                          </Link>
                        </CardTitle>
                        <CardDescription className="truncate">
                          {[story.chineseTitle, story.author].filter(Boolean).join(" · ") || "作者信息未提供"}
                        </CardDescription>
                      </div>
                      <CardAction>
                        <StatusPill status={story.status} />
                      </CardAction>
                    </CardHeader>
                    {showProgress && (
                      <CardContent>
                        <div className="flex flex-col gap-2 rounded-lg bg-muted/60 p-3">
                          <TranslateProgressBar progress={story.progress} thin />
                          <TranslateProgressLegend progress={story.progress} />
                        </div>
                      </CardContent>
                    )}
                    <Separator />
                    <CardFooter className="mt-auto flex flex-wrap items-center gap-2">
                      <span className="mr-auto text-xs tabular-nums text-muted-foreground">
                        {story.chapterCount} 章 · {story.wordCount.toLocaleString()} 字
                      </span>
                      <TranslationStatusButton storyID={story.id} />
                      {canRetry && (
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={actionPending}
                          onClick={() => retryStory(story.id)}
                        >
                          {retrying ? (
                            <Spinner data-icon="inline-start" />
                          ) : (
                            <RotateCcw data-icon="inline-start" />
                          )}
                          {retrying ? "重试中" : "重试"}
                        </Button>
                      )}
                      {user && (
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          disabled={actionPending}
                          onClick={() => setDeleteTarget({ id: story.id, title: story.title })}
                          aria-label={`删除 ${story.title}`}
                        >
                          <Trash2 data-icon="inline-start" />
                        </Button>
                      )}
                    </CardFooter>
                  </Card>
                </li>
              );
            })}
          </ul>
        </section>
      )}

      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 />
            </AlertDialogMedia>
            <AlertDialogTitle>删除这篇作品？</AlertDialogTitle>
            <AlertDialogDescription>
              「{deleteTarget?.title}」及其翻译结果会从本地书架中永久移除，此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={del.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={del.isPending}
              onClick={(event) => {
                event.preventDefault();
                if (deleteTarget) deleteStory(deleteTarget.id);
              }}
            >
              {del.isPending && <Spinner data-icon="inline-start" />}
              {del.isPending ? "删除中" : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex items-center justify-between rounded-lg border bg-background/60 px-4 py-3">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-2xl font-semibold tabular-nums text-primary">{value}</span>
    </div>
  );
}

function LibrarySkeleton() {
  return (
    <div className="flex flex-col gap-8">
      <Card>
        <CardHeader>
          <Skeleton className="h-6 w-32" />
          <Skeleton className="h-9 w-2/3" />
          <Skeleton className="h-5 w-full max-w-xl" />
        </CardHeader>
        <CardContent>
          <div className="grid gap-3 sm:grid-cols-3">
            {Array.from({ length: 3 }).map((_, index) => (
              <Skeleton key={index} className="h-16 w-full" />
            ))}
          </div>
        </CardContent>
      </Card>
      <div className="grid gap-4 lg:grid-cols-2">
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index}>
            <CardHeader>
              <Skeleton className="h-5 w-16" />
              <Skeleton className="h-6 w-4/5" />
              <Skeleton className="h-4 w-2/5" />
            </CardHeader>
            <CardFooter>
              <Skeleton className="h-8 w-full" />
            </CardFooter>
          </Card>
        ))}
      </div>
    </div>
  );
}
