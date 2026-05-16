import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

export function Version() {
  const qc = useQueryClient();
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["version"],
    queryFn: () => api.version(),
  });
  const apply = useMutation({
    mutationFn: (force: boolean) => api.applyUpdate(force),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["version"] }),
  });

  if (isLoading) return <p className="text-muted">读取版本…</p>;
  if (!data) return null;

  const latest = data.latest;
  return (
    <div className="mx-auto max-w-[640px] space-y-8 fade-in">
      <header>
        <h1 className="text-[clamp(2rem,5vw,3rem)] font-semibold tracking-tight">
          Version
        </h1>
      </header>

      <dl className="grid grid-cols-[140px_1fr] gap-y-3 text-[14px]">
        <dt className="text-muted">Current</dt>
        <dd className="font-mono">{data.current}</dd>
        <dt className="text-muted">Platform</dt>
        <dd className="font-mono">
          {data.platform}/{data.arch}
        </dd>
        {data.builtAt && (
          <>
            <dt className="text-muted">Built</dt>
            <dd className="font-mono">{data.builtAt}</dd>
          </>
        )}
      </dl>

      <section className="border-t rule pt-6 space-y-4">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted">
            Remote
          </h2>
          <button
            type="button"
            className="btn btn-ghost"
            onClick={() => refetch()}
            disabled={isFetching}
          >
            {isFetching ? "检查中…" : "重新检查"}
          </button>
        </div>
        {!latest && (
          <p className="text-muted text-[13px]">
            未配置 manifest URL，或暂时无法访问。去 Settings 配置后再来。
          </p>
        )}
        {latest && (
          <div className="space-y-3">
            <div className="flex items-baseline gap-3">
              <span className="font-mono text-[16px]">{latest.version}</span>
              {latest.hasUpdate ? (
                <span className="chip chip-accent">有新版</span>
              ) : (
                <span className="chip">已最新</span>
              )}
            </div>
            {latest.notes && (
              <pre className="whitespace-pre-wrap rounded-2xl border rule bg-surface p-4 font-mono text-[12px] leading-relaxed">
                {latest.notes}
              </pre>
            )}
            {latest.publishedAt && (
              <p className="text-muted text-[12px]">发布于 {latest.publishedAt}</p>
            )}
            <div className="flex gap-2">
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => apply.mutate(false)}
                disabled={!latest.hasUpdate || apply.isPending}
              >
                {apply.isPending ? "下载安装中…" : "下载并安装"}
              </button>
              {!latest.hasUpdate && (
                <button
                  type="button"
                  className="btn btn-ghost"
                  onClick={() => apply.mutate(true)}
                  disabled={apply.isPending}
                >
                  强制重装当前版本
                </button>
              )}
            </div>
            {apply.data && (
              <p
                className={`text-[12px] ${
                  apply.data.ok ? "text-emerald-500" : "text-red-500"
                }`}
              >
                {apply.data.message}
              </p>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
