import { FormEvent, useEffect, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useAuth } from "../lib/auth";

export function LoginPage() {
  const navigate = useNavigate();
  const { user, needsSetup, loading, login } = useAuth();
  const search = useSearch({ strict: false }) as { redirect?: string };
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (loading) return;
    if (needsSetup) {
      navigate({ to: "/setup", replace: true });
      return;
    }
    if (user) {
      navigate({ to: search.redirect ?? "/", replace: true });
    }
  }, [user, needsSetup, loading, navigate, search.redirect]);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(username.trim(), password);
      navigate({ to: search.redirect ?? "/", replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto max-w-[400px] space-y-8 fade-in">
      <header className="space-y-2 text-center">
        <h1 className="text-[clamp(2rem,5vw,2.8rem)] font-semibold tracking-tight">
          登录
        </h1>
        <p className="text-muted-foreground text-[13px]">
          需要登录才能导入和管理作品。
        </p>
      </header>
      <form onSubmit={onSubmit} className="space-y-5">
        <div className="space-y-1.5">
          <Label htmlFor="login-username">用户名</Label>
          <Input
            id="login-username"
            autoComplete="username"
            autoFocus
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
            disabled={submitting}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="login-password">密码</Label>
          <Input
            id="login-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            disabled={submitting}
          />
        </div>
        {error && (
          <p className="text-destructive text-[12px]">{error}</p>
        )}
        <Button
          type="submit"
          variant="default"
          size="lg"
          className="w-full gap-1.5"
          disabled={submitting || !username.trim() || !password}
        >
          <LogIn className="size-3.5" />
          {submitting ? "登录中…" : "登录"}
        </Button>
      </form>
    </div>
  );
}
