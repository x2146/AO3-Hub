import { FormEvent, useEffect, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { BookOpenText, LogIn, ShieldCheck } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
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
    if (user) navigate({ to: search.redirect ?? "/", replace: true });
  }, [user, needsSetup, loading, navigate, search.redirect]);

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
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
    <div className="mx-auto flex min-h-[calc(100svh-11rem)] w-full max-w-md items-center fade-in">
      <Card className="w-full">
        <CardHeader className="gap-4 text-center">
          <div className="mx-auto flex size-12 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-sm">
            <BookOpenText />
          </div>
          <div className="flex flex-col gap-1.5">
            <CardTitle>欢迎回来</CardTitle>
            <CardDescription>登录后导入作品、管理翻译并继续阅读。</CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          <form id="login-form" onSubmit={onSubmit}>
            <FieldGroup>
              <Field data-disabled={submitting || undefined}>
                <FieldLabel htmlFor="login-username">用户名</FieldLabel>
                <Input
                  id="login-username"
                  autoComplete="username"
                  autoFocus
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  placeholder="输入用户名"
                  required
                  disabled={submitting}
                />
              </Field>
              <Field data-disabled={submitting || undefined}>
                <FieldLabel htmlFor="login-password">密码</FieldLabel>
                <Input
                  id="login-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="输入密码"
                  required
                  disabled={submitting}
                />
              </Field>
              {error && (
                <Alert variant="destructive">
                  <ShieldCheck />
                  <AlertTitle>无法登录</AlertTitle>
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
            </FieldGroup>
          </form>
        </CardContent>
        <CardFooter className="flex flex-col gap-3">
          <Button
            form="login-form"
            type="submit"
            size="lg"
            className="w-full"
            disabled={submitting || !username.trim() || !password}
          >
            {submitting ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <LogIn data-icon="inline-start" />
            )}
            {submitting ? "登录中" : "登录"}
          </Button>
          <p className="text-center text-xs text-muted-foreground">
            账号由本机管理员创建，登录信息仅保存在当前 AO3 Hub 实例。
          </p>
        </CardFooter>
      </Card>
    </div>
  );
}
