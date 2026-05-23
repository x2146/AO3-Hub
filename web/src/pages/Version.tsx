import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import type { ApplyUpdateRequest } from "@ao3hub/shared";
import { api } from "../lib/api";

export function Version() {
  const qc = useQueryClient();
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["version"],
    queryFn: () => api.version(),
  });
  const apply = useMutation({
    mutationFn: (body: ApplyUpdateRequest) => api.applyUpdate(body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["version"] }),
  });

  if (isLoading) return <p className="text-muted-foreground">读取版本…</p>;
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
        <dt className="text-muted-foreground">Current</dt>
        <dd className="font-mono">{data.current}</dd>
        <dt className="text-muted-foreground">Platform</dt>
        <dd className="font-mono">
          {data.platform}/{data.arch}
        </dd>
        {data.builtAt && (
          <>
            <dt className="text-muted-foreground">Built</dt>
            <dd className="font-mono">{data.builtAt}</dd>
          </>
        )}
      </dl>

      <Separator />

      <section className="space-y-4">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted-foreground">
            Remote
          </h2>
          <Button
            variant="outline"
            size="sm"
            onClick={() => refetch()}
            disabled={isFetching}
            className="gap-1.5"
          >
            <RefreshCw className={isFetching ? "size-3.5 animate-spin" : "size-3.5"} />
            {isFetching ? "检查中…" : "重新检查"}
          </Button>
        </div>
        {!latest && (
          <p className="text-muted-foreground text-[13px]">
            未配置 manifest URL，或暂时无法访问。去 Settings 配置后再来。
          </p>
        )}
        {latest && (
          <div className="space-y-3">
            <div className="flex items-baseline gap-3">
              <span className="font-mono text-[16px]">{latest.version}</span>
              {latest.hasUpdate ? (
                <Badge variant="accent">有新版</Badge>
              ) : (
                <Badge>已最新</Badge>
              )}
            </div>
            {latest.notes && (
              <pre className="whitespace-pre-wrap rounded-2xl border border-border bg-surface/60 p-4 font-mono text-[12px] leading-relaxed">
                {latest.notes}
              </pre>
            )}
            {latest.publishedAt && (
              <p className="text-muted-foreground text-[12px]">
                发布于 {latest.publishedAt}
              </p>
            )}
            <div className="flex gap-2">
              <Button
                variant="default"
                onClick={() => apply.mutate({})}
                disabled={!latest.hasUpdate || apply.isPending}
              >
                {apply.isPending ? "下载安装中…" : "下载并安装"}
              </Button>
              <Button
                variant="outline"
                onClick={() =>
                  apply.mutate({ force: true, forceVersion: latest.version })
                }
                disabled={apply.isPending}
              >
                强制拉取此版本
              </Button>
            </div>
            {apply.data && (
              <p
                className={`text-[12px] ${
                  apply.data.ok ? "text-success" : "text-destructive"
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
