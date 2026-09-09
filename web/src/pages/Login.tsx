import { FormEvent, useEffect, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Eye, EyeOff, LogIn } from "lucide-react";
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
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Spinner } from "@/components/ui/spinner";
import { BrandMark } from "@/components/BrandMark";
import { useAuth } from "../lib/auth";

export function LoginPage() {
  const navigate = useNavigate();
  const { user, needsSetup, loading, login } = useAuth();
  const search = useSearch({ strict: false }) as { redirect?: string };
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
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
    <div className="fade-in mx-auto flex min-h-[calc(100svh-11rem)] w-full max-w-sm items-center">
      <Card className="w-full">
        <CardHeader className="items-center text-center">
          <BrandMark
            className="mx-auto mb-2 size-10 rounded-xl"
            glyphClassName="size-5"
          />
          <CardTitle className="text-lg">欢迎回来</CardTitle>
          <CardDescription>
            登录后导入作品、管理翻译并继续阅读。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form id="login-form" onSubmit={onSubmit}>
            <FieldGroup>
              <Field
                data-disabled={submitting || undefined}
                data-invalid={!!error || undefined}
              >
                <FieldLabel htmlFor="login-username">用户名</FieldLabel>
                <Input
                  id="login-username"
                  autoComplete="username"
                  autoFocus
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  aria-invalid={!!error || undefined}
                  placeholder="输入用户名"
                  required
                  disabled={submitting}
                />
              </Field>
              <Field
                data-disabled={submitting || undefined}
                data-invalid={!!error || undefined}
              >
                <FieldLabel htmlFor="login-password">密码</FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id="login-password"
                    type={showPassword ? "text" : "password"}
                    autoComplete="current-password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    aria-invalid={!!error || undefined}
                    placeholder="输入密码"
                    required
                    disabled={submitting}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton
                      size="icon-xs"
                      aria-label={showPassword ? "隐藏密码" : "显示密码"}
                      onClick={() => setShowPassword((v) => !v)}
                      disabled={submitting}
                    >
                      {showPassword ? <EyeOff /> : <Eye />}
                    </InputGroupButton>
                  </InputGroupAddon>
                </InputGroup>
              </Field>
              {error && (
                <Alert variant="destructive">
                  <AlertTitle>无法登录</AlertTitle>
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
            </FieldGroup>
          </form>
        </CardContent>
        <CardFooter className="flex-col gap-3">
          <Button
            form="login-form"
            type="submit"
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
