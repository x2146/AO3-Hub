import { Link } from "@tanstack/react-router";
import { useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowUpRight,
  BookOpenText,
  FilePlus2,
  LibraryBig,
  ListFilter,
  LogIn,
  MoreHorizontal,
  RotateCcw,
  SearchIcon,
  Trash2,
  XIcon,
} from "lucide-react";
import type { StoryListItem, StoryStatus } from "@ao3hub/shared";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Kbd } from "@/components/ui/kbd";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
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

type StatusFilter = "all" | "ready" | "working" | "error";
type SortKey = "updated" | "added" | "title" | "progress";

const STATUS_FILTERS: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "全部状态" },
  { value: "ready", label: "可阅读" },
  { value: "working", label: "处理中" },
  { value: "error", label: "有错误" },
];

const SORTS: { value: SortKey; label: string }[] = [
  { value: "updated", label: "最近更新" },
  { value: "added", label: "最近添加" },
  { value: "title", label: "标题 A→Z" },
  { value: "progress", label: "翻译进度" },
];

const matchesStatus = (story: StoryListItem, filter: StatusFilter) => {
  switch (filter) {
    case "ready":
      return story.status === "ready";
    case "working":
      return isInFlight(story.status);
    case "error":
      return story.status === "error" || breakdownOf(story.progress).error > 0;
    default:
      return true;
  }
};

const progressRatio = (story: StoryListItem) => {
  const { total, done } = breakdownOf(story.progress);
  if (story.status === "ready") return 1;
  return total > 0 ? done / total : 0;
};

export function Library() {
  const qc = useQueryClient();
  const actionPendingRef = useRef(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [sort, setSort] = useState<SortKey>("updated");
  const deferredQuery = useDeferredValue(query);
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

  // `/` jumps to the filter box the way it does on AO3 itself.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.isContentEditable ||
          ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
      ) {
        return;
      }
      event.preventDefault();
      searchRef.current?.focus();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const del = useMutation({
    mutationFn: (id: string) => api.remove(id),
    onSuccess: (_result, id) => {
      const title = deleteTarget?.id === id ? deleteTarget.title : "作品";
      setDeleteTarget(null);
      toast.success(`已删除「${title}」`);
      return qc.invalidateQueries({ queryKey: ["stories"] });
    },
    onError: (deleteError) =>
      toast.error("删除失败", { description: deleteError.message }),
    onSettled: () => {
      actionPendingRef.current = false;
    },
  });

  const retry = useMutation({
    mutationFn: (id: string) => api.retry(id, {}),
    onSuccess: () => {
      toast.success("已重新入队", { description: "失败的段落会重新翻译。" });
      return qc.invalidateQueries({ queryKey: ["stories"] });
    },
    onError: (retryError) =>
      toast.error("重试失败", { description: retryError.message }),
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

  const stories = useMemo(() => data?.stories ?? [], [data]);

  const visible = useMemo(() => {
    const needle = deferredQuery.trim().toLowerCase();
    const filtered = stories.filter((story) => {
      if (!matchesStatus(story, statusFilter)) return false;
      if (!needle) return true;
      return [story.title, story.chineseTitle, story.author]
        .filter(Boolean)
        .some((field) => field!.toLowerCase().includes(needle));
    });

    return [...filtered].sort((a, b) => {
      switch (sort) {
        case "added":
          return b.addedAt.localeCompare(a.addedAt);
        case "title":
          return a.title.localeCompare(b.title, "en");
        case "progress":
          return progressRatio(b) - progressRatio(a);
        default:
          return b.updatedAt.localeCompare(a.updatedAt);
      }
    });
  }, [stories, deferredQuery, statusFilter, sort]);

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

  const readyCount = stories.filter((story) => story.status === "ready").length;
  const inFlightCount = stories.filter((story) => isInFlight(story.status)).length;
  const errorCount = stories.filter(
    (story) => story.status === "error" || breakdownOf(story.progress).error > 0,
  ).length;
  const isFiltered = statusFilter !== "all" || deferredQuery.trim() !== "";

  return (
    <div className="fade-in flex flex-col gap-6">
      <header className="flex flex-col gap-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div className="flex min-w-0 flex-col gap-2">
            <h1 className="cn-font-heading text-2xl font-semibold tracking-tight sm:text-3xl">
              你的 AO3 阅读书架
            </h1>
            <p className="max-w-2xl text-sm text-muted-foreground">
              集中管理作品、翻译进度与阅读状态。上传 AO3 HTML，或直接粘贴作品链接开始翻译。
            </p>
          </div>
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
        </div>

        {stories.length > 0 && (
          <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Metric label="全部作品" value={stories.length} />
            <Metric label="可阅读" value={readyCount} tone="success" />
            <Metric label="处理中" value={inFlightCount} tone="primary" />
            <Metric label="有错误" value={errorCount} tone="destructive" />
          </dl>
        )}
      </header>

      {stories.length === 0 ? (
        <Empty className="min-h-[380px] border">
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
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <h2 id="works-heading" className="sr-only">
              全部作品
            </h2>
            <InputGroup className="sm:max-w-xs">
              <InputGroupAddon>
                <SearchIcon />
              </InputGroupAddon>
              <InputGroupInput
                ref={searchRef}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") setQuery("");
                }}
                placeholder="搜索标题或作者"
                aria-label="搜索作品"
              />
              <InputGroupAddon align="inline-end">
                {query ? (
                  <InputGroupButton
                    size="icon-xs"
                    aria-label="清除搜索"
                    onClick={() => {
                      setQuery("");
                      searchRef.current?.focus();
                    }}
                  >
                    <XIcon />
                  </InputGroupButton>
                ) : (
                  <Kbd>/</Kbd>
                )}
              </InputGroupAddon>
            </InputGroup>

            <div className="flex items-center gap-2">
              <Select
                value={statusFilter}
                onValueChange={(value) => setStatusFilter(value as StatusFilter)}
              >
                <SelectTrigger aria-label="按状态筛选" className="w-[8.5rem]">
                  <ListFilter />
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper" align="start">
                  <SelectGroup>
                    {STATUS_FILTERS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Select
                value={sort}
                onValueChange={(value) => setSort(value as SortKey)}
              >
                <SelectTrigger aria-label="排序方式" className="w-[8.5rem]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent position="popper" align="start">
                  <SelectGroup>
                    {SORTS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <p
              aria-live="polite"
              className="text-xs tabular-nums text-muted-foreground sm:ml-auto"
            >
              {visible.length} / {stories.length} 篇
            </p>
          </div>

          {visible.length === 0 ? (
            <Empty className="min-h-64 border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchIcon />
                </EmptyMedia>
                <EmptyTitle>没有符合条件的作品</EmptyTitle>
                <EmptyDescription>
                  换个关键词，或把筛选条件放宽一些。
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setQuery("");
                    setStatusFilter("all");
                  }}
                >
                  <RotateCcw data-icon="inline-start" />
                  重置筛选
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <ul className="grid gap-4 xl:grid-cols-2">
              {visible.map((story) => (
                <li key={story.id} className="min-w-0">
                  <StoryCard
                    story={story}
                    canManage={!!user}
                    actionPending={actionPending}
                    retrying={retry.isPending && retry.variables === story.id}
                    onRetry={() => retryStory(story.id)}
                    onDelete={() =>
                      setDeleteTarget({ id: story.id, title: story.title })
                    }
                  />
                </li>
              ))}
            </ul>
          )}

          {isFiltered && visible.length > 0 && (
            <p className="text-xs text-muted-foreground">
              已按条件筛选 ·{" "}
              <button
                type="button"
                className="underline underline-offset-4 hover:text-foreground"
                onClick={() => {
                  setQuery("");
                  setStatusFilter("all");
                }}
              >
                显示全部
              </button>
            </p>
          )}
        </section>
      )}

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
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

function StoryCard({
  story,
  canManage,
  actionPending,
  retrying,
  onRetry,
  onDelete,
}: {
  story: StoryListItem;
  canManage: boolean;
  actionPending: boolean;
  retrying: boolean;
  onRetry: () => void;
  onDelete: () => void;
}) {
  const { error: errorBlocks } = breakdownOf(story.progress);
  const showProgress =
    story.progress &&
    (story.status !== "ready" ||
      (story.progress.totalBlocks ?? 0) > (story.progress.doneBlocks ?? 0));
  const canRetry = canManage && (story.status === "error" || errorBlocks > 0);
  const subtitle =
    [story.chineseTitle, story.author].filter(Boolean).join(" · ") ||
    "作者信息未提供";

  return (
    <Card className="h-full transition-shadow hover:shadow-md">
      <CardHeader>
        <CardTitle>
          <Link
            to="/r/$id/$chapter"
            params={{ id: story.id, chapter: "0" }}
            className="line-clamp-2 rounded-sm outline-none hover:text-primary focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            {story.title}
          </Link>
        </CardTitle>
        <CardDescription className="line-clamp-1">{subtitle}</CardDescription>
        <CardAction>
          <StatusPill status={story.status} />
        </CardAction>
      </CardHeader>

      <CardContent className="flex flex-col gap-3">
        {story.status === "error" && story.progress?.message && (
          <p className="text-sm text-destructive">{story.progress.message}</p>
        )}
        {showProgress ? (
          <div className="flex flex-col gap-2">
            <TranslateProgressBar progress={story.progress} thin />
            <TranslateProgressLegend progress={story.progress} />
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            翻译已完成，随时可以开始阅读。
          </p>
        )}
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline">{story.chapterCount} 章</Badge>
          <Badge variant="outline">{story.wordCount.toLocaleString()} 字</Badge>
          {errorBlocks > 0 && (
            <Badge variant="destructive">{errorBlocks} 段失败</Badge>
          )}
        </div>
      </CardContent>

      <CardFooter className="gap-2">
        <Button size="sm" variant="outline" asChild>
          <Link to="/r/$id/$chapter" params={{ id: story.id, chapter: "0" }}>
            <BookOpenText data-icon="inline-start" />
            阅读
          </Link>
        </Button>
        <TranslationStatusButton storyID={story.id} />
        {canRetry && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                disabled={actionPending}
                onClick={onRetry}
              >
                {retrying ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <RotateCcw data-icon="inline-start" />
                )}
                {retrying ? "重试中" : "重试"}
              </Button>
            </TooltipTrigger>
            <TooltipContent>重新翻译所有失败段落</TooltipContent>
          </Tooltip>
        )}
        {canManage && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                className="ml-auto"
                disabled={actionPending}
                aria-label={`「${story.title}」的更多操作`}
              >
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-44">
              <DropdownMenuGroup>
                <DropdownMenuItem asChild>
                  <Link
                    to="/r/$id/$chapter"
                    params={{ id: story.id, chapter: "0" }}
                  >
                    <BookOpenText />
                    打开阅读器
                  </Link>
                </DropdownMenuItem>
                {canRetry && (
                  <DropdownMenuItem onSelect={onRetry}>
                    <RotateCcw />
                    重新翻译失败段落
                  </DropdownMenuItem>
                )}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                  <Trash2 />
                  删除作品
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </CardFooter>
    </Card>
  );
}

const METRIC_TONE = {
  default: "text-foreground",
  primary: "text-primary",
  success: "text-success",
  destructive: "text-destructive",
} as const;

function Metric({
  label,
  value,
  tone = "default",
}: {
  label: string;
  value: number;
  tone?: keyof typeof METRIC_TONE;
}) {
  return (
    <div className="flex flex-col gap-0.5 rounded-lg border bg-card px-3 py-2.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd
        className={cn(
          "text-xl font-semibold tabular-nums",
          value === 0 ? METRIC_TONE.default : METRIC_TONE[tone],
        )}
      >
        {value}
      </dd>
    </div>
  );
}

function LibrarySkeleton() {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-5 w-full max-w-xl" />
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className="h-[4.25rem] w-full" />
        ))}
      </div>
      <Skeleton className="h-8 w-full max-w-md" />
      <div className="grid gap-4 xl:grid-cols-2">
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index}>
            <CardHeader>
              <Skeleton className="h-5 w-4/5" />
              <Skeleton className="h-4 w-2/5" />
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <Skeleton className="h-1 w-full" />
              <Skeleton className="h-4 w-3/5" />
            </CardContent>
            <CardFooter>
              <Skeleton className="h-7 w-40" />
            </CardFooter>
          </Card>
        ))}
      </div>
    </div>
  );
}
