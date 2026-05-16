import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ShieldCheck } from "lucide-react";
import { PASSWORD_MIN, USERNAME_RE } from "@ao3hub/shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useAuth } from "../lib/auth";

export function SetupPage() {
  const navigate = useNavigate();
  const { needsSetup, user, loading, setup } = useAuth();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (loading) return;
    if (!needsSetup) {
      navigate({ to: user ? "/" : "/login", replace: true });
    }
  }, [needsSetup, user, loading, navigate]);

  const onSubmit = async (e: FormEvent) => {
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
    if (password !== confirm) {
      setError("两次输入的密码不一致");
      return;
    }
    setSubmitting(true);
    try {
      await setup(username.trim(), password);
      navigate({ to: "/", replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "初始化失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto max-w-[460px] space-y-8 fade-in">
      <header className="space-y-2 text-center">
        <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-accent/10 text-accent">
          <ShieldCheck className="size-5" />
        </div>
        <h1 className="text-[clamp(2rem,5vw,2.8rem)] font-semibold tracking-tight">
          初始化管理员
        </h1>
        <p className="text-muted-foreground text-[13px] leading-relaxed">
          创建首个 admin 账号。后续可在「用户」页添加更多用户。
        </p>
      </header>
      <form onSubmit={onSubmit} className="space-y-5">
        <div className="space-y-1.5">
          <Label htmlFor="setup-username">用户名</Label>
          <Input
            id="setup-username"
            autoComplete="username"
            autoFocus
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
            disabled={submitting}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="setup-password">密码</Label>
          <Input
            id="setup-password"
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            disabled={submitting}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="setup-confirm">确认密码</Label>
          <Input
            id="setup-confirm"
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            required
            disabled={submitting}
          />
        </div>
        {error && <p className="text-destructive text-[12px]">{error}</p>}
        <Button
          type="submit"
          variant="default"
          size="lg"
          className="w-full"
          disabled={submitting || !username || !password || !confirm}
        >
          {submitting ? "创建中…" : "创建管理员"}
        </Button>
      </form>
    </div>
  );
}
