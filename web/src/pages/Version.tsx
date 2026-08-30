import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Download, PackageCheck, RefreshCw, ServerCog } from "lucide-react";
import type { ApplyUpdateRequest } from "@ao3hub/shared";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Separator } from "@/components/ui/separator";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";

export function Version() {
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["version"],
    queryFn: ({ signal }) => api.version(signal),
  });
  const check = useMutation({
    mutationFn: () => api.checkUpdate(),
    onSuccess: (next) => queryClient.setQueryData(["version"], next),
  });
  const apply = useMutation({
    mutationFn: (body: ApplyUpdateRequest) => api.applyUpdate(body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["version"] }),
  });
  const isChecking = isFetching || check.isPending;
  const actionPending = isChecking || apply.isPending;

  if (isLoading) return <VersionSkeleton />;

  if (error || !data) {
    return (
      <Alert variant="destructive">
        <ServerCog />
        <AlertTitle>无法读取版本信息</AlertTitle>
        <AlertDescription>
          {error instanceof Error ? error.message : "未知错误"}
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            重试
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  const latest = data.latest;
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-8 fade-in">
      <div className="flex flex-col gap-2">
        <Badge variant="accent">
          <PackageCheck />
          Release channel
        </Badge>
        <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">版本与更新</h1>
        <p className="text-sm text-muted-foreground">
          查看当前构建、远程发行状态，并在管理员授权下执行自更新。
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex flex-col gap-1.5">
            <CardTitle>当前实例</CardTitle>
            <CardDescription>正在运行的 AO3 Hub 二进制信息。</CardDescription>
          </div>
          <CardAction>
            <Badge variant="success">
              <CheckCircle2 />
              Running
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-3 sm:grid-cols-3">
            <VersionMetric label="版本" value={data.current} />
            <VersionMetric label="平台" value={`${data.platform}/${data.arch}`} />
            <VersionMetric label="构建时间" value={data.builtAt || "未提供"} />
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex flex-col gap-1.5">
            <CardTitle>远程发行</CardTitle>
            <CardDescription>来自已配置 Manifest 的最新可用版本。</CardDescription>
          </div>
          <CardAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                if (user?.role === "admin") check.mutate();
                else void refetch();
              }}
              disabled={actionPending}
            >
              {isChecking ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <RefreshCw data-icon="inline-start" />
              )}
              {isChecking ? "检查中" : user?.role === "admin" ? "检查更新" : "刷新"}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {!latest ? (
            <Alert>
              <ServerCog />
              <AlertTitle>暂时没有远程版本信息</AlertTitle>
              <AlertDescription>
                Manifest URL 可能尚未配置或当前无法访问。管理员可前往设置页面检查更新源。
              </AlertDescription>
            </Alert>
          ) : (
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center gap-3">
                <span className="break-all font-mono text-lg font-semibold">{latest.version}</span>
                <Badge variant={latest.hasUpdate ? "accent" : "success"}>
                  {latest.hasUpdate ? "发现新版" : "已是最新"}
                </Badge>
              </div>
              {(latest.strategy || latest.updateReason) && (
                <p className="text-sm text-muted-foreground">
                  {[latest.strategy, latest.updateReason].filter(Boolean).join(" · ")}
                </p>
              )}
              {latest.notes && (
                <pre className="max-w-full whitespace-pre-wrap break-words rounded-lg border bg-muted/60 p-4 font-mono text-xs leading-relaxed [overflow-wrap:anywhere]">
                  {latest.notes}
                </pre>
              )}
              {latest.publishedAt && (
                <p className="text-xs text-muted-foreground">发布于 {latest.publishedAt}</p>
              )}
            </div>
          )}

          {check.isError && (
            <Alert variant="destructive" className="mt-4">
              <RefreshCw />
              <AlertTitle>检查更新失败</AlertTitle>
              <AlertDescription>
                {check.error instanceof Error ? check.error.message : "未知错误"}
              </AlertDescription>
            </Alert>
          )}
        </CardContent>
        {latest && user?.role === "admin" && (
          <>
            <Separator />
            <CardFooter className="flex flex-wrap gap-2">
              <Button
                onClick={() => apply.mutate({})}
                disabled={!latest.hasUpdate || actionPending}
              >
                {apply.isPending ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <Download data-icon="inline-start" />
                )}
                {apply.isPending ? "下载安装中" : "下载并安装"}
              </Button>
              <Button
                variant="outline"
                onClick={() => apply.mutate({ force: true, forceVersion: latest.version })}
                disabled={actionPending}
              >
                强制拉取此版本
              </Button>
            </CardFooter>
          </>
        )}
      </Card>

      {apply.data && (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>更新任务已提交</AlertTitle>
          <AlertDescription>{apply.data.message}</AlertDescription>
        </Alert>
      )}
      {apply.isError && (
        <Alert variant="destructive">
          <Download />
          <AlertTitle>更新失败</AlertTitle>
          <AlertDescription>
            {apply.error instanceof Error ? apply.error.message : "未知错误"}
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}

function VersionMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg bg-muted/60 p-3">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-all font-mono text-sm font-medium">{value}</dd>
    </div>
  );
}

function VersionSkeleton() {
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <Skeleton className="h-9 w-56" />
      {Array.from({ length: 2 }).map((_, index) => (
        <Card key={index}>
          <CardHeader>
            <Skeleton className="h-6 w-32" />
            <Skeleton className="h-4 w-2/3" />
          </CardHeader>
          <CardContent>
            <Skeleton className="h-24 w-full" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
