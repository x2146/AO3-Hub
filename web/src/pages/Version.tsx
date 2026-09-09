import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  CheckCircle2,
  Download,
  PackageCheck,
  RefreshCw,
  ServerCog,
  Sparkles,
} from "lucide-react";
import type { ApplyUpdateRequest } from "@ao3hub/shared";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { PageHeader } from "@/components/PageHeader";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";

type PendingApply = { force: boolean; version: string };

export function Version() {
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const [confirm, setConfirm] = useState<PendingApply | null>(null);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["version"],
    queryFn: ({ signal }) => api.version(signal),
  });

  const check = useMutation({
    mutationFn: () => api.checkUpdate(),
    onSuccess: (next) => {
      queryClient.setQueryData(["version"], next);
      toast.success(
        next.latest?.hasUpdate
          ? `发现新版本 ${next.latest.version}`
          : "已经是最新版本",
      );
    },
    onError: (checkError) =>
      toast.error("检查更新失败", {
        description:
          checkError instanceof Error ? checkError.message : "未知错误",
      }),
  });

  const apply = useMutation({
    mutationFn: (body: ApplyUpdateRequest) => api.applyUpdate(body),
    onSuccess: (result) => {
      toast.success("更新任务已提交", { description: result.message });
      queryClient.invalidateQueries({ queryKey: ["version"] });
    },
    onError: (applyError) =>
      toast.error("更新失败", {
        description:
          applyError instanceof Error ? applyError.message : "未知错误",
      }),
  });

  const isChecking = isFetching || check.isPending;
  const actionPending = isChecking || apply.isPending;
  const isAdmin = user?.role === "admin";

  if (isLoading) return <VersionSkeleton />;

  if (error || !data) {
    return (
      <Alert variant="destructive">
        <ServerCog />
        <AlertTitle>无法读取版本信息</AlertTitle>
        <AlertDescription className="flex flex-col items-start gap-3">
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
    <div className="fade-in mx-auto flex w-full max-w-3xl flex-col gap-6">
      <PageHeader
        title="版本"
        description="查看当前构建与远程发行状态。更新包通过 sha256 校验后替换二进制并自动重启。"
      />

      <Card>
        <CardHeader>
          <CardTitle>当前实例</CardTitle>
          <CardDescription>正在运行的 AO3 Hub 二进制信息。</CardDescription>
          <CardAction>
            <Badge variant="success">
              <CheckCircle2 />
              运行中
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-3 sm:gap-6">
            <VersionMetric label="版本" value={data.current} />
            <VersionMetric
              label="平台"
              value={`${data.platform}/${data.arch}`}
            />
            <VersionMetric
              label="构建时间"
              value={formatBuiltAt(data.builtAt)}
            />
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>远程发行</CardTitle>
          <CardDescription>
            来自已配置 Manifest 的最新可用版本。
          </CardDescription>
          <CardAction>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                if (isAdmin) check.mutate();
                else void refetch();
              }}
              disabled={actionPending}
            >
              {isChecking ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <RefreshCw data-icon="inline-start" />
              )}
              {isChecking ? "检查中" : isAdmin ? "检查更新" : "刷新"}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {!latest ? (
            <Alert>
              <ServerCog />
              <AlertTitle>暂时没有远程版本信息</AlertTitle>
              <AlertDescription>
                Manifest URL
                可能尚未配置或当前无法访问。管理员可前往设置页面检查更新源。
              </AlertDescription>
            </Alert>
          ) : (
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-lg font-semibold break-all">
                  {latest.version}
                </span>
                <Badge variant={latest.hasUpdate ? "accent" : "success"}>
                  {latest.hasUpdate ? <Sparkles /> : <CheckCircle2 />}
                  {latest.hasUpdate ? "发现新版" : "已是最新"}
                </Badge>
                {latest.channel && (
                  <Badge variant="outline">{latest.channel}</Badge>
                )}
              </div>
              {(latest.strategy || latest.updateReason) && (
                <p className="text-sm text-muted-foreground">
                  {[latest.strategy, latest.updateReason]
                    .filter(Boolean)
                    .join(" · ")}
                </p>
              )}
              {latest.notes && (
                <pre className="max-w-full rounded-lg border bg-muted/50 p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
                  {latest.notes}
                </pre>
              )}
              {latest.publishedAt && (
                <p className="text-xs text-muted-foreground">
                  发布于 {latest.publishedAt}
                </p>
              )}
            </div>
          )}
        </CardContent>
        {latest && isAdmin && (
          <CardFooter className="flex-wrap gap-2">
            <Button
              onClick={() =>
                setConfirm({ force: false, version: latest.version })
              }
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
              variant="ghost"
              onClick={() =>
                setConfirm({ force: true, version: latest.version })
              }
              disabled={actionPending}
            >
              强制重新安装
            </Button>
            <span className="ml-auto text-xs text-muted-foreground">
              安装完成后服务会自动重启
            </span>
          </CardFooter>
        )}
      </Card>

      <AlertDialog
        open={!!confirm}
        onOpenChange={(open) => !open && setConfirm(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <PackageCheck />
            </AlertDialogMedia>
            <AlertDialogTitle>
              {confirm?.force ? "强制拉取并安装？" : "现在安装更新？"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              将下载 {confirm?.version}，校验 sha256
              后替换当前二进制并重启服务。
              重启期间页面会短暂断开，正在进行的翻译任务会中断。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={apply.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={apply.isPending}
              onClick={(event) => {
                event.preventDefault();
                if (!confirm) return;
                apply.mutate(
                  confirm.force
                    ? { force: true, forceVersion: confirm.version }
                    : {},
                );
                setConfirm(null);
              }}
            >
              {apply.isPending && <Spinner data-icon="inline-start" />}
              确认安装
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** Build stamps arrive as RFC 3339; a raw ISO string wraps badly in the tile. */
function formatBuiltAt(value: string | undefined): string {
  if (!value) return "未提供";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function VersionMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="font-mono text-sm font-medium break-all">{value}</dd>
    </div>
  );
}

function VersionSkeleton() {
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-8 w-24" />
        <Skeleton className="h-5 w-3/4" />
      </div>
      {Array.from({ length: 2 }).map((_, index) => (
        <Card key={index}>
          <CardHeader>
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-4 w-2/3" />
          </CardHeader>
          <CardContent>
            <Skeleton className="h-20 w-full" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
