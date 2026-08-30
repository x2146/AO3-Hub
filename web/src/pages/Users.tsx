import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, ShieldCheck, Trash2, UserPlus, UsersRound } from "lucide-react";
import type { PublicUser, Role } from "@ao3hub/shared";
import { PASSWORD_MIN, USERNAME_RE } from "@ao3hub/shared";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";

export function UsersPage() {
  const navigate = useNavigate();
  const { user: me, loading } = useAuth();
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["users"],
    queryFn: ({ signal }) => api.listUsers(signal),
    enabled: !!me && me.role === "admin",
  });

  useEffect(() => {
    if (loading) return;
    if (!me) {
      navigate({ to: "/login", search: { redirect: "/users" }, replace: true });
      return;
    }
    if (me.role !== "admin") navigate({ to: "/", replace: true });
  }, [me, loading, navigate]);

  const create = useMutation({
    mutationFn: (input: { username: string; password: string; role: Role }) =>
      api.createUser(input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["users"] }),
  });

  const update = useMutation({
    mutationFn: (input: { id: string; password?: string; role?: Role }) =>
      api.updateUser(input.id, { password: input.password, role: input.role }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["users"] }),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteUser(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["users"] }),
  });

  const [showCreate, setShowCreate] = useState(false);
  const [resetTarget, setResetTarget] = useState<PublicUser | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<PublicUser | null>(null);
  const mutating = create.isPending || update.isPending || remove.isPending;

  if (loading || !me || me.role !== "admin") return null;

  const users = data?.users ?? [];
  const adminCount = users.filter((user) => user.role === "admin").length;

  return (
    <div className="flex flex-col gap-8 fade-in">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex flex-col gap-2">
          <Badge variant="accent">
            <UsersRound />
            Access control
          </Badge>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">用户管理</h1>
          <p className="max-w-2xl text-sm text-muted-foreground">
            管理本机账号与权限。普通用户可以导入和阅读，管理员还可管理用户及服务配置。
          </p>
        </div>
        <Button onClick={() => setShowCreate(true)} disabled={mutating}>
          <UserPlus data-icon="inline-start" />
          新建用户
        </Button>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <Metric label="全部用户" value={users.length} />
        <Metric label="管理员" value={adminCount} />
      </div>

      {isLoading ? (
        <UsersSkeleton />
      ) : error ? (
        <Alert variant="destructive">
          <UsersRound />
          <AlertTitle>用户列表加载失败</AlertTitle>
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      ) : users.length === 0 ? (
        <Empty className="min-h-72">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <UsersRound />
            </EmptyMedia>
            <EmptyTitle>还没有用户</EmptyTitle>
            <EmptyDescription>创建第一个用户，让其他人也能使用这个 AO3 Hub 实例。</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button onClick={() => setShowCreate(true)}>
              <UserPlus data-icon="inline-start" />
              新建用户
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2">
          {users.map((user) => (
            <li key={user.id}>
              <Card className="h-full">
                <CardHeader>
                  <div className="flex min-w-0 flex-col gap-1.5 pr-20">
                    <CardTitle className="truncate">{user.username}</CardTitle>
                    <CardDescription>创建于 {user.createdAt.slice(0, 10)}</CardDescription>
                  </div>
                  <CardAction className="flex items-center gap-1.5">
                    {user.id === me.id && <Badge variant="outline">你</Badge>}
                    <Badge variant={user.role === "admin" ? "accent" : "secondary"}>
                      {user.role}
                    </Badge>
                  </CardAction>
                </CardHeader>
                <CardContent>
                  <div className="rounded-lg bg-muted/60 p-3 text-sm text-muted-foreground">
                    {user.role === "admin"
                      ? "可管理用户、翻译服务与更新配置"
                      : "可导入作品、查看翻译状态并阅读"}
                  </div>
                </CardContent>
                <Separator />
                <CardFooter className="mt-auto flex flex-wrap items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      update.mutate({
                        id: user.id,
                        role: user.role === "admin" ? "user" : "admin",
                      })
                    }
                    disabled={mutating || user.id === me.id}
                    title={user.id === me.id ? "不能修改自己的角色" : undefined}
                  >
                    设为 {user.role === "admin" ? "user" : "admin"}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setResetTarget(user)}
                    disabled={mutating}
                    aria-label={`重置 ${user.username} 的密码`}
                  >
                    <KeyRound data-icon="inline-start" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setDeleteTarget(user)}
                    disabled={mutating || user.id === me.id}
                    aria-label={`删除 ${user.username}`}
                  >
                    <Trash2 data-icon="inline-start" />
                  </Button>
                </CardFooter>
              </Card>
            </li>
          ))}
        </ul>
      )}

      {(create.error || update.error || remove.error) && (
        <Alert variant="destructive">
          <ShieldCheck />
          <AlertTitle>操作失败</AlertTitle>
          <AlertDescription>
            {(create.error ?? update.error ?? remove.error)?.message}
          </AlertDescription>
        </Alert>
      )}

      <CreateDialog
        open={showCreate}
        onOpenChange={setShowCreate}
        onSubmit={async (value) => {
          await create.mutateAsync(value);
          setShowCreate(false);
        }}
        pending={mutating}
      />
      <ResetDialog
        target={resetTarget}
        onOpenChange={(open) => !open && setResetTarget(null)}
        onSubmit={async (password) => {
          if (!resetTarget) return;
          await update.mutateAsync({ id: resetTarget.id, password });
          setResetTarget(null);
        }}
        pending={mutating}
      />
      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 />
            </AlertDialogMedia>
            <AlertDialogTitle>删除用户？</AlertDialogTitle>
            <AlertDialogDescription>
              「{deleteTarget?.username}」将无法再登录，所有现有会话也会失效。此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={remove.isPending}
              onClick={(event) => {
                event.preventDefault();
                if (!deleteTarget) return;
                remove.mutate(deleteTarget.id, { onSuccess: () => setDeleteTarget(null) });
              }}
            >
              {remove.isPending && <Spinner data-icon="inline-start" />}
              {remove.isPending ? "删除中" : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CreateDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  onSubmit: (value: { username: string; password: string; role: Role }) => Promise<void>;
  pending: boolean;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("user");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setUsername("");
      setPassword("");
      setRole("user");
      setError(null);
    }
  }, [open]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!USERNAME_RE.test(username.trim())) {
      setError("用户名只允许字母、数字、下划线、短横线，3–32 字符");
      return;
    }
    if (password.length < PASSWORD_MIN) {
      setError(`密码至少 ${PASSWORD_MIN} 个字符`);
      return;
    }
    try {
      await onSubmit({ username: username.trim(), password, role });
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "创建失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建用户</DialogTitle>
          <DialogDescription>创建可登录当前 AO3 Hub 实例的本地账号。</DialogDescription>
        </DialogHeader>
        <form id="create-user-form" onSubmit={submit}>
          <FieldGroup>
            <Field data-disabled={pending || undefined}>
              <FieldLabel htmlFor="create-username">用户名</FieldLabel>
              <Input
                id="create-username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                autoFocus
                required
                disabled={pending}
              />
            </Field>
            <Field data-disabled={pending || undefined}>
              <FieldLabel htmlFor="create-password">初始密码</FieldLabel>
              <Input
                id="create-password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
                disabled={pending}
              />
              <FieldDescription>至少 {PASSWORD_MIN} 个字符。</FieldDescription>
            </Field>
            <FieldSet data-disabled={pending || undefined}>
              <FieldLegend variant="label">角色</FieldLegend>
              <ToggleGroup
                type="single"
                value={role}
                onValueChange={(value) => value && setRole(value as Role)}
                variant="outline"
                className="w-full"
                disabled={pending}
              >
                <ToggleGroupItem value="user" className="flex-1">普通用户</ToggleGroupItem>
                <ToggleGroupItem value="admin" className="flex-1">管理员</ToggleGroupItem>
              </ToggleGroup>
            </FieldSet>
            {error && (
              <Alert variant="destructive">
                <AlertTitle>无法创建用户</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button
            form="create-user-form"
            type="submit"
            disabled={pending || !username || !password}
          >
            {pending && <Spinner data-icon="inline-start" />}
            {pending ? "创建中" : "创建用户"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ResetDialog({
  target,
  onOpenChange,
  onSubmit,
  pending,
}: {
  target: PublicUser | null;
  onOpenChange: (value: boolean) => void;
  onSubmit: (password: string) => Promise<void>;
  pending: boolean;
}) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!target) {
      setPassword("");
      setError(null);
    }
  }, [target]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (password.length < PASSWORD_MIN) {
      setError(`密码至少 ${PASSWORD_MIN} 个字符`);
      return;
    }
    try {
      await onSubmit(password);
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "重置失败");
    }
  };

  return (
    <Dialog open={!!target} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>重置「{target?.username}」的密码</DialogTitle>
          <DialogDescription>保存后，该用户的所有现有会话都会立即失效。</DialogDescription>
        </DialogHeader>
        <form id="reset-password-form" onSubmit={submit}>
          <FieldGroup>
            <Field
              data-invalid={!!error || undefined}
              data-disabled={pending || undefined}
            >
              <FieldLabel htmlFor="reset-password">新密码</FieldLabel>
              <Input
                id="reset-password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                aria-invalid={!!error || undefined}
                autoFocus
                required
                disabled={pending}
              />
              <FieldDescription>至少 {PASSWORD_MIN} 个字符。</FieldDescription>
              {error && <FieldError>{error}</FieldError>}
            </Field>
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={pending}>
            取消
          </Button>
          <Button
            form="reset-password-form"
            type="submit"
            disabled={pending || !password}
          >
            {pending && <Spinner data-icon="inline-start" />}
            {pending ? "保存中" : "保存新密码"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{value}</CardTitle>
        <CardDescription>{label}</CardDescription>
      </CardHeader>
    </Card>
  );
}

function UsersSkeleton() {
  return (
    <div className="grid gap-4 md:grid-cols-2">
      {Array.from({ length: 4 }).map((_, index) => (
        <Card key={index}>
          <CardHeader>
            <Skeleton className="h-6 w-1/2" />
            <Skeleton className="h-4 w-1/3" />
          </CardHeader>
          <CardContent>
            <Skeleton className="h-14 w-full" />
          </CardContent>
          <CardFooter>
            <Skeleton className="h-8 w-full" />
          </CardFooter>
        </Card>
      ))}
    </div>
  );
}
