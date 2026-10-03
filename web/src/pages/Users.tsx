import { Fragment, FormEvent, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  KeyRound,
  MoreHorizontal,
  ShieldCheck,
  Trash2,
  UserPlus,
  UserRound,
  UsersRound,
} from "lucide-react";
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
  CardContent,
  CardDescription,
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
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemSeparator,
  ItemTitle,
} from "@/components/ui/item";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { PageHeader } from "@/components/PageHeader";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";

const ROLE_LABEL: Record<Role, string> = { admin: "管理员", user: "普通用户" };

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

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["users"] });
  const onMutationError = (label: string) => (mutationError: Error) =>
    toast.error(label, { description: mutationError.message });

  const create = useMutation({
    mutationFn: (input: { username: string; password: string; role: Role }) =>
      api.createUser(input),
    onSuccess: (result) => {
      toast.success(`已创建用户「${result.user.username}」`);
      return invalidate();
    },
    onError: onMutationError("创建用户失败"),
  });

  const update = useMutation({
    mutationFn: (input: { id: string; password?: string; role?: Role }) =>
      api.updateUser(input.id, { password: input.password, role: input.role }),
    onSuccess: (result, input) => {
      toast.success(
        input.password
          ? `已重置「${result.user.username}」的密码`
          : `「${result.user.username}」现在是${ROLE_LABEL[result.user.role]}`,
      );
      return invalidate();
    },
    onError: onMutationError("更新用户失败"),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteUser(id),
    onSuccess: () => {
      toast.success("用户已删除");
      return invalidate();
    },
    onError: onMutationError("删除用户失败"),
  });

  const [showCreate, setShowCreate] = useState(false);
  const [resetTarget, setResetTarget] = useState<PublicUser | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<PublicUser | null>(null);
  const mutating = create.isPending || update.isPending || remove.isPending;

  if (loading || !me || me.role !== "admin") return null;

  const users = data?.users ?? [];
  const adminCount = users.filter((user) => user.role === "admin").length;

  return (
    <div className="fade-in flex w-full max-w-4xl flex-col gap-6">
      <PageHeader
        title="用户"
        description="普通用户可以导入和阅读，管理员还可以管理用户与服务配置。"
        actions={
          <Button onClick={() => setShowCreate(true)} disabled={mutating}>
            <UserPlus data-icon="inline-start" />
            新建用户
          </Button>
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>账号</CardTitle>
          <CardDescription>
            共 {users.length} 个账号，其中 {adminCount} 位管理员。
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <UsersSkeleton />
          ) : error ? (
            <Alert variant="destructive">
              <UsersRound />
              <AlertTitle>用户列表加载失败</AlertTitle>
              <AlertDescription>{error.message}</AlertDescription>
            </Alert>
          ) : users.length === 0 ? (
            <Empty className="min-h-56 border border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <UsersRound />
                </EmptyMedia>
                <EmptyTitle>还没有用户</EmptyTitle>
                <EmptyDescription>
                  创建第一个用户，让其他人也能使用这个 AO3 Hub 实例。
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={() => setShowCreate(true)}>
                  <UserPlus data-icon="inline-start" />
                  新建用户
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <ItemGroup className="gap-0">
              {users.map((user, index) => {
                const isSelf = user.id === me.id;
                return (
                  <Fragment key={user.id}>
                    {index > 0 && <ItemSeparator className="my-0" />}
                    <Item role="listitem" className="px-1">
                      <ItemMedia
                        variant="icon"
                        className="size-8 rounded-lg bg-muted text-muted-foreground"
                      >
                        {user.role === "admin" ? (
                          <ShieldCheck />
                        ) : (
                          <UserRound />
                        )}
                      </ItemMedia>
                      <ItemContent>
                        <ItemTitle>
                          {user.username}
                          <Badge
                            variant={
                              user.role === "admin" ? "accent" : "secondary"
                            }
                          >
                            {ROLE_LABEL[user.role]}
                          </Badge>
                          {isSelf && <Badge variant="outline">你</Badge>}
                        </ItemTitle>
                        <ItemDescription>
                          创建于 {user.createdAt.slice(0, 10)} ·{" "}
                          {user.role === "admin"
                            ? "可管理用户、翻译服务与更新"
                            : "可导入作品并阅读"}
                        </ItemDescription>
                      </ItemContent>
                      <ItemActions>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              disabled={mutating}
                              aria-label={`「${user.username}」的操作`}
                            >
                              <MoreHorizontal />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end" className="min-w-48">
                            <DropdownMenuGroup>
                              <DropdownMenuItem
                                disabled={isSelf}
                                onSelect={() =>
                                  update.mutate({
                                    id: user.id,
                                    role:
                                      user.role === "admin" ? "user" : "admin",
                                  })
                                }
                              >
                                <ShieldCheck />
                                {user.role === "admin"
                                  ? "降为普通用户"
                                  : "设为管理员"}
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onSelect={() => setResetTarget(user)}
                              >
                                <KeyRound />
                                重置密码
                              </DropdownMenuItem>
                            </DropdownMenuGroup>
                            <DropdownMenuSeparator />
                            <DropdownMenuGroup>
                              <DropdownMenuItem
                                variant="destructive"
                                disabled={isSelf}
                                onSelect={() => setDeleteTarget(user)}
                              >
                                <Trash2 />
                                删除用户
                              </DropdownMenuItem>
                            </DropdownMenuGroup>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </ItemActions>
                    </Item>
                  </Fragment>
                );
              })}
            </ItemGroup>
          )}
        </CardContent>
      </Card>

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
      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 />
            </AlertDialogMedia>
            <AlertDialogTitle>删除用户？</AlertDialogTitle>
            <AlertDialogDescription>
              「{deleteTarget?.username}
              」将无法再登录，所有现有会话也会失效。此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={remove.isPending}
              onClick={(event) => {
                event.preventDefault();
                if (!deleteTarget) return;
                remove.mutate(deleteTarget.id, {
                  onSuccess: () => setDeleteTarget(null),
                });
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
  onSubmit: (value: {
    username: string;
    password: string;
    role: Role;
  }) => Promise<void>;
  pending: boolean;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("user");
  const [invalidField, setInvalidField] = useState<
    "username" | "password" | null
  >(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setUsername("");
      setPassword("");
      setRole("user");
      setInvalidField(null);
      setError(null);
    }
  }, [open]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    setInvalidField(null);
    if (!USERNAME_RE.test(username.trim())) {
      setInvalidField("username");
      setError("用户名只允许字母、数字、下划线、短横线，3–32 字符");
      return;
    }
    if (password.length < PASSWORD_MIN) {
      setInvalidField("password");
      setError(`密码至少 ${PASSWORD_MIN} 个字符`);
      return;
    }
    try {
      await onSubmit({ username: username.trim(), password, role });
    } catch {
      // The mutation surfaces its own toast; the dialog stays open for a retry.
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建用户</DialogTitle>
          <DialogDescription>
            创建可登录当前 AO3 Hub 实例的本地账号。
          </DialogDescription>
        </DialogHeader>
        <form id="create-user-form" onSubmit={submit}>
          <FieldGroup>
            <Field
              data-disabled={pending || undefined}
              data-invalid={invalidField === "username" || undefined}
            >
              <FieldLabel htmlFor="create-username">用户名</FieldLabel>
              <Input
                id="create-username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                aria-invalid={invalidField === "username" || undefined}
                autoComplete="off"
                autoFocus
                required
                disabled={pending}
              />
              {invalidField === "username" ? (
                <FieldError>{error}</FieldError>
              ) : (
                <FieldDescription>
                  3–32 个字符，可使用字母、数字、下划线和短横线。
                </FieldDescription>
              )}
            </Field>
            <Field
              data-disabled={pending || undefined}
              data-invalid={invalidField === "password" || undefined}
            >
              <FieldLabel htmlFor="create-password">初始密码</FieldLabel>
              <Input
                id="create-password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                aria-invalid={invalidField === "password" || undefined}
                autoComplete="new-password"
                required
                disabled={pending}
              />
              {invalidField === "password" ? (
                <FieldError>{error}</FieldError>
              ) : (
                <FieldDescription>
                  至少 {PASSWORD_MIN} 个字符。
                </FieldDescription>
              )}
            </Field>
            <FieldSet data-disabled={pending || undefined}>
              <FieldLegend variant="label">角色</FieldLegend>
              <RadioGroup
                value={role}
                onValueChange={(value) => setRole(value as Role)}
                disabled={pending}
              >
                <RoleOption
                  value="user"
                  title={ROLE_LABEL.user}
                  description="可以导入作品、查看翻译状态并阅读。"
                />
                <RoleOption
                  value="admin"
                  title={ROLE_LABEL.admin}
                  description="还可以管理其他用户、服务配置与 OTA 更新。"
                />
              </RadioGroup>
            </FieldSet>
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
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

function RoleOption({
  value,
  title,
  description,
}: {
  value: Role;
  title: string;
  description: string;
}) {
  const id = `create-role-${value}`;
  return (
    <FieldLabel htmlFor={id}>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldTitle>{title}</FieldTitle>
          <FieldDescription>{description}</FieldDescription>
        </FieldContent>
        <RadioGroupItem value={value} id={id} />
      </Field>
    </FieldLabel>
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
    } catch {
      // The mutation surfaces its own toast; the dialog stays open for a retry.
    }
  };

  return (
    <Dialog open={!!target} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>重置「{target?.username}」的密码</DialogTitle>
          <DialogDescription>
            保存后，该用户的所有现有会话都会立即失效。
          </DialogDescription>
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
                autoComplete="new-password"
                autoFocus
                required
                disabled={pending}
              />
              {error ? (
                <FieldError>{error}</FieldError>
              ) : (
                <FieldDescription>
                  至少 {PASSWORD_MIN} 个字符。
                </FieldDescription>
              )}
            </Field>
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
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

function UsersSkeleton() {
  return (
    <div className="flex flex-col divide-y">
      {Array.from({ length: 3 }).map((_, index) => (
        <div key={index} className="flex items-center gap-3 px-1 py-2.5">
          <Skeleton className="size-8 rounded-lg" />
          <div className="flex flex-1 flex-col gap-1.5">
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-3 w-52" />
          </div>
          <Skeleton className="size-7" />
        </div>
      ))}
    </div>
  );
}
