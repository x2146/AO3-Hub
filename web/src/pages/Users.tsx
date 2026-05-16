import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Trash2, UserPlus } from "lucide-react";
import type { PublicUser, Role } from "@ao3hub/shared";
import { PASSWORD_MIN, USERNAME_RE } from "@ao3hub/shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";

export function UsersPage() {
  const navigate = useNavigate();
  const { user: me, loading } = useAuth();
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["users"],
    queryFn: () => api.listUsers(),
    enabled: !!me && me.role === "admin",
  });

  useEffect(() => {
    if (loading) return;
    if (!me) {
      navigate({ to: "/login", search: { redirect: "/users" }, replace: true });
      return;
    }
    if (me.role !== "admin") {
      navigate({ to: "/", replace: true });
    }
  }, [me, loading, navigate]);

  const create = useMutation({
    mutationFn: (input: { username: string; password: string; role: Role }) =>
      api.createUser(input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const update = useMutation({
    mutationFn: (input: { id: string; password?: string; role?: Role }) =>
      api.updateUser(input.id, { password: input.password, role: input.role }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteUser(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const [showCreate, setShowCreate] = useState(false);
  const [resetTarget, setResetTarget] = useState<PublicUser | null>(null);

  if (loading || !me || me.role !== "admin") return null;

  const users = data?.users ?? [];

  return (
    <div className="mx-auto max-w-[760px] space-y-10 fade-in">
      <header className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-[clamp(2rem,5vw,3rem)] font-semibold tracking-tight">
            Users
          </h1>
          <p className="text-muted-foreground mt-2 text-[14px]">
            管理 admin / user 账号。user 不能访问「Settings」「Users」。
          </p>
        </div>
        <Button variant="default" onClick={() => setShowCreate(true)} className="gap-1.5">
          <UserPlus className="size-3.5" /> 新建
        </Button>
      </header>

      {isLoading ? (
        <p className="text-muted-foreground">载入用户…</p>
      ) : error ? (
        <p className="text-destructive">加载失败：{(error as Error).message}</p>
      ) : users.length === 0 ? (
        <p className="text-muted-foreground">还没有用户。</p>
      ) : (
        <ul>
          {users.map((u, i) => (
            <li key={u.id}>
              {i > 0 && <Separator />}
              <div className="group grid grid-cols-[1fr_auto] items-center gap-4 py-5">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <p className="truncate text-[18px] font-semibold tracking-tight">
                      {u.username}
                    </p>
                    {u.role === "admin" ? (
                      <Badge variant="accent">admin</Badge>
                    ) : (
                      <Badge>user</Badge>
                    )}
                    {u.id === me.id && (
                      <span className="text-muted-foreground text-[11px]">（你）</span>
                    )}
                  </div>
                  <p className="text-muted-foreground mt-0.5 text-[12px] tabular-nums">
                    创建于 {u.createdAt.slice(0, 10)}
                  </p>
                </div>
                <div className="flex items-center gap-1">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      update.mutate({
                        id: u.id,
                        role: u.role === "admin" ? "user" : "admin",
                      })
                    }
                    disabled={update.isPending || u.id === me.id}
                    title={u.id === me.id ? "不能修改自己的角色" : ""}
                  >
                    设为 {u.role === "admin" ? "user" : "admin"}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => setResetTarget(u)}
                    aria-label="重置密码"
                  >
                    <KeyRound className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => {
                      if (u.id === me.id) return;
                      if (confirm(`删除用户「${u.username}」？`)) remove.mutate(u.id);
                    }}
                    disabled={u.id === me.id}
                    aria-label="删除"
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}

      {(create.error || update.error || remove.error) && (
        <p className="text-destructive text-[12px]">
          {((create.error || update.error || remove.error) as Error).message}
        </p>
      )}

      <CreateDialog
        open={showCreate}
        onOpenChange={setShowCreate}
        onSubmit={async (v) => {
          await create.mutateAsync(v);
          setShowCreate(false);
        }}
        pending={create.isPending}
      />
      <ResetDialog
        target={resetTarget}
        onOpenChange={(open) => !open && setResetTarget(null)}
        onSubmit={async (password) => {
          if (!resetTarget) return;
          await update.mutateAsync({ id: resetTarget.id, password });
          setResetTarget(null);
        }}
        pending={update.isPending}
      />
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
  onOpenChange: (v: boolean) => void;
  onSubmit: (v: { username: string; password: string; role: Role }) => Promise<void>;
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

  const submit = async (e: FormEvent) => {
    e.preventDefault();
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
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建用户</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="cu-username">用户名</Label>
            <Input
              id="cu-username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoFocus
              required
              disabled={pending}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="cu-password">初始密码</Label>
            <Input
              id="cu-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              disabled={pending}
            />
          </div>
          <div className="space-y-1.5">
            <Label>角色</Label>
            <div className="flex gap-2">
              {(["user", "admin"] as Role[]).map((r) => (
                <Button
                  key={r}
                  type="button"
                  variant={role === r ? "default" : "outline"}
                  size="sm"
                  onClick={() => setRole(r)}
                >
                  {r}
                </Button>
              ))}
            </div>
          </div>
          {error && <p className="text-destructive text-[12px]">{error}</p>}
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              取消
            </Button>
            <Button
              type="submit"
              variant="default"
              disabled={pending || !username || !password}
            >
              {pending ? "创建中…" : "创建"}
            </Button>
          </DialogFooter>
        </form>
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
  onOpenChange: (v: boolean) => void;
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

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < PASSWORD_MIN) {
      setError(`密码至少 ${PASSWORD_MIN} 个字符`);
      return;
    }
    try {
      await onSubmit(password);
    } catch (err) {
      setError(err instanceof Error ? err.message : "重置失败");
    }
  };

  return (
    <Dialog open={!!target} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>重置「{target?.username}」的密码</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="rp-password">新密码</Label>
            <Input
              id="rp-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoFocus
              required
              disabled={pending}
            />
            <p className="text-muted-foreground text-[12px]">
              重置后该用户的所有 session 都会失效。
            </p>
          </div>
          {error && <p className="text-destructive text-[12px]">{error}</p>}
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              取消
            </Button>
            <Button type="submit" variant="default" disabled={pending || !password}>
              {pending ? "保存中…" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
