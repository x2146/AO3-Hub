import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { StatusPill } from "../components/StatusPill";

export function Library() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["stories"],
    queryFn: () => api.listStories(),
    refetchInterval: (q) => {
      const stories = (q.state.data as Awaited<ReturnType<typeof api.listStories>> | undefined)?.stories;
      const inFlight = stories?.some((s) =>
        ["queued", "fetching", "parsing", "translating"].includes(s.status),
      );
      return inFlight ? 3000 : false;
    },
  });

  const del = useMutation({
    mutationFn: (id: string) => api.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["stories"] }),
  });

  if (isLoading) {
    return <p className="text-muted">载入书架…</p>;
  }
  if (error) {
    return <p className="text-red-500">加载失败：{(error as Error).message}</p>;
  }

  const stories = data?.stories ?? [];
  return (
    <div className="space-y-10 fade-in">
      <header className="flex flex-wrap items-end justify-between gap-6 border-b rule pb-7">
        <div>
          <h1 className="text-[clamp(2.4rem,8vw,4.5rem)] font-semibold leading-[0.95] tracking-tight">
            AO3<span className="text-muted">.</span>Hub
          </h1>
          <p className="text-muted mt-3 max-w-[560px] text-[15px] leading-relaxed">
            个人 AO3 翻译 + 阅读 CMS。贴 work URL，或者直接拖 AO3 导出的 HTML 进来。
          </p>
        </div>
        <div className="text-right">
          <p className="text-[clamp(2.4rem,5vw,3.6rem)] leading-none font-semibold text-accent">
            {stories.length}
          </p>
          <p className="text-muted text-[12px] tracking-wider uppercase mt-1">
            {stories.length === 1 ? "work" : "works"}
          </p>
        </div>
      </header>

      <ul className="divide-y rule">
        {stories.length === 0 && (
          <li className="py-20 text-center">
            <p className="text-muted">书架是空的。</p>
            <Link to="/import" className="btn btn-primary mt-5 inline-flex">
              添加第一篇
            </Link>
          </li>
        )}
        {stories.map((s, i) => (
          <li
            key={s.id}
            className="group grid grid-cols-[44px_minmax(0,1fr)_auto] items-center gap-5 py-6 transition-transform hover:translate-x-1"
          >
            <span className="text-muted font-mono text-[12px]">
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
              <p className="text-muted mt-1 truncate text-[13px]">
                {[s.chineseTitle, s.author].filter(Boolean).join(" · ") || "—"}
              </p>
            </Link>
            <div className="flex items-center gap-3">
              <span className="text-muted text-[12px] tabular-nums">
                {s.chapterCount} ch · {s.wordCount.toLocaleString()} w
              </span>
              <StatusPill status={s.status} />
              <button
                type="button"
                className="btn btn-ghost text-[12px] opacity-0 transition-opacity group-hover:opacity-100"
                onClick={() => {
                  if (confirm(`删除「${s.title}」？`)) del.mutate(s.id);
                }}
                aria-label="删除"
              >
                删除
              </button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
