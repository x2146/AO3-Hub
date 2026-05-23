import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RotateCcw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { api, type StoriesListResponse } from "../lib/api";
import { useAuth } from "../lib/auth";
import { StatusPill } from "../components/StatusPill";
import {
  TranslateProgressBar,
  TranslateProgressLegend,
  breakdownOf,
} from "../components/TranslateProgress";

export function Library() {
  const qc = useQueryClient();
  const { user } = useAuth();
  const { data: config } = useQuery({
    queryKey: ["config", "public"],
    queryFn: () => api.getPublicConfig(),
  });
  const { data, isLoading, error } = useQuery({
    queryKey: ["stories"],
    queryFn: () => api.listStories(),
    refetchInterval: (q) => {
      const stories = (q.state.data as StoriesListResponse | undefined)?.stories;
      const inFlight = stories?.some((s) =>
        ["queued", "fetching", "parsing", "translating"].includes(s.status),
      );
      return inFlight ? config?.ui.libraryRefetchIntervalMs ?? 3000 : false;
    },
  });

  const del = useMutation({
    mutationFn: (id: string) => api.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["stories"] }),
  });

  const retry = useMutation({
    mutationFn: (id: string) => api.retry(id, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["stories"] }),
  });

  if (isLoading) {
    return <p className="text-muted-foreground">载入书架…</p>;
  }
  if (error) {
    return (
      <p className="text-destructive">
        加载失败：{(error as Error).message}
      </p>
    );
  }

  const stories = data?.stories ?? [];
  return (
    <div className="space-y-10 fade-in">
      <header className="flex flex-wrap items-end justify-between gap-6 border-b border-border pb-7">
        <div>
          <h1 className="text-[clamp(2.4rem,8vw,4.5rem)] font-semibold leading-[0.95] tracking-tight">
            AO3<span className="text-muted-foreground">.</span>Hub
          </h1>
          <p className="text-muted-foreground mt-3 max-w-[560px] text-[15px] leading-relaxed">
            个人 AO3 翻译 + 阅读 CMS。贴 work URL，或者直接拖 AO3 导出的 HTML 进来。
          </p>
        </div>
        <div className="text-right">
          <p className="text-[clamp(2.4rem,5vw,3.6rem)] leading-none font-semibold text-accent">
            {stories.length}
          </p>
          <p className="text-muted-foreground text-[12px] tracking-wider uppercase mt-1">
            {stories.length === 1 ? "work" : "works"}
          </p>
        </div>
      </header>

      {stories.length === 0 ? (
        <div className="py-20 text-center">
          <p className="text-muted-foreground">书架是空的。</p>
          {user ? (
            <Button variant="default" size="lg" asChild className="mt-5">
              <Link to="/import">添加第一篇</Link>
            </Button>
          ) : (
            <Button variant="outline" size="lg" asChild className="mt-5">
              <Link to="/login" search={{ redirect: undefined }}>登录后导入</Link>
            </Button>
          )}
        </div>
      ) : (
        <ul>
          {stories.map((s, i) => {
            const showProgress =
              s.progress &&
              (s.status !== "ready" ||
                (s.progress.totalBlocks ?? 0) > (s.progress.doneBlocks ?? 0));
            const { error: errorCount } = breakdownOf(s.progress);
            const canRetry = !!user && (s.status === "error" || errorCount > 0);
            const retrying = retry.isPending && retry.variables === s.id;
            return (
              <li key={s.id}>
                {i > 0 && <Separator />}
                <div className="group grid grid-cols-[44px_minmax(0,1fr)_auto] items-center gap-5 py-6 transition-transform hover:translate-x-1">
                  <span className="text-muted-foreground font-mono text-[12px]">
                    {String(i + 1).padStart(2, "0")}
                  </span>
                  <Link
                    to="/r/$id/$chapter"
                    params={{ id: s.id, chapter: "0" }}
                    className="min-w-0"
                  >
                    <p className="truncate text-[clamp(1.3rem,3.2vw,2rem)] font-semibold leading-[1.1] tracking-tight">
                      {s.title}
                    </p>
                    <p className="text-muted-foreground mt-1 truncate text-[13px]">
                      {[s.chineseTitle, s.author].filter(Boolean).join(" · ") ||
                        "—"}
                    </p>
                    {showProgress && (
                      <div className="mt-3 space-y-1.5">
                        <TranslateProgressBar progress={s.progress} thin />
                        <TranslateProgressLegend progress={s.progress} />
                      </div>
                    )}
                  </Link>
                  <div className="flex items-center gap-3">
                    <span className="text-muted-foreground text-[12px] tabular-nums">
                      {s.chapterCount} ch · {s.wordCount.toLocaleString()} w
                    </span>
                    <StatusPill status={s.status} />
                    {canRetry && (
                      <Button
                        variant="outline"
                        size="sm"
                        className="gap-1"
                        disabled={retrying}
                        onClick={() => retry.mutate(s.id)}
                      >
                        <RotateCcw className="size-3" />
                        {retrying ? "重试中…" : "重试失败"}
                      </Button>
                    )}
                    {user && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="opacity-0 transition-opacity group-hover:opacity-100"
                        onClick={() => {
                          if (confirm(`删除「${s.title}」？`)) del.mutate(s.id);
                        }}
                        aria-label="删除"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
