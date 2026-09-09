import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ShieldCheck, UserRoundCheck } from "lucide-react";
import { PASSWORD_MIN, USERNAME_RE } from "@ao3hub/shared";
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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { BrandMark } from "@/components/BrandMark";
import { useAuth } from "../lib/auth";

type InvalidField = "username" | "password" | "confirm" | "form" | null;

export function SetupPage() {
  const navigate = useNavigate();
  const { needsSetup, user, loading, setup } = useAuth();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [invalidField, setInvalidField] = useState<InvalidField>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (loading) return;
    if (!needsSetup) navigate({ to: user ? "/" : "/login", replace: true });
  }, [needsSetup, user, loading, navigate]);

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    setInvalidField(null);
    if (!USERNAME_RE.test(username.trim())) {
      setInvalidField("username");
      setError("用户名只允许字母、数字、下划线、短横线，长度为 3–32 字符");
      return;
    }
    if (password.length < PASSWORD_MIN) {
      setInvalidField("password");
      setError(`密码至少需要 ${PASSWORD_MIN} 个字符`);
      return;
    }
    if (password !== confirm) {
      setInvalidField("confirm");
      setError("两次输入的密码不一致");
      return;
    }
    setSubmitting(true);
    try {
      await setup(username.trim(), password);
      navigate({ to: "/", replace: true });
    } catch (err) {
      setInvalidField("form");
      setError(err instanceof Error ? err.message : "初始化失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="fade-in mx-auto flex min-h-[calc(100svh-11rem)] w-full max-w-md items-center">
      <Card className="w-full">
        <CardHeader className="items-center text-center">
          <BrandMark
            className="mx-auto mb-2 size-10 rounded-xl"
            glyphClassName="size-5"
          />
          <CardTitle className="text-lg">初始化 AO3 Hub</CardTitle>
          <CardDescription>
            创建第一个管理员账号。完成后可在用户页面继续添加普通用户或管理员。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form id="setup-form" onSubmit={onSubmit}>
            <FieldGroup>
              <Field
                data-invalid={invalidField === "username" || undefined}
                data-disabled={submitting || undefined}
              >
                <FieldLabel htmlFor="setup-username">管理员用户名</FieldLabel>
                <Input
                  id="setup-username"
                  autoComplete="username"
                  autoFocus
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  aria-invalid={invalidField === "username" || undefined}
                  required
                  disabled={submitting}
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
                data-invalid={invalidField === "password" || undefined}
                data-disabled={submitting || undefined}
              >
                <FieldLabel htmlFor="setup-password">密码</FieldLabel>
                <Input
                  id="setup-password"
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  aria-invalid={invalidField === "password" || undefined}
                  required
                  disabled={submitting}
                />
                {invalidField === "password" ? (
                  <FieldError>{error}</FieldError>
                ) : (
                  <FieldDescription>
                    至少 {PASSWORD_MIN} 个字符。
                  </FieldDescription>
                )}
              </Field>
              <Field
                data-invalid={invalidField === "confirm" || undefined}
                data-disabled={submitting || undefined}
              >
                <FieldLabel htmlFor="setup-confirm">确认密码</FieldLabel>
                <Input
                  id="setup-confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(event) => setConfirm(event.target.value)}
                  aria-invalid={invalidField === "confirm" || undefined}
                  required
                  disabled={submitting}
                />
                {invalidField === "confirm" && <FieldError>{error}</FieldError>}
              </Field>
              {invalidField === "form" && error && (
                <Alert variant="destructive">
                  <ShieldCheck />
                  <AlertTitle>初始化失败</AlertTitle>
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
            </FieldGroup>
          </form>
        </CardContent>
        <CardFooter>
          <Button
            form="setup-form"
            type="submit"
            className="w-full"
            disabled={submitting || !username || !password || !confirm}
          >
            {submitting ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <UserRoundCheck data-icon="inline-start" />
            )}
            {submitting ? "创建中" : "创建管理员"}
          </Button>
        </CardFooter>
      </Card>
    </div>
  );
}
