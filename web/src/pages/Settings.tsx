import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DEFAULT_DEV_UPDATE_MANIFEST_URL,
  DEFAULT_UPDATE_MANIFEST_URL,
  CONFIG_LIMITS,
  Config as ConfigSchema,
  type Config,
} from "@ao3hub/shared";
import { Check, Cpu, ServerCog, Settings2, X } from "lucide-react";
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
  Field as FieldPrimitive,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { api, type ConfigUpdate } from "../lib/api";
import { useAuth } from "../lib/auth";

type LocalConfig = Config;

const defaultManifestURLForChannel = (channel: string) =>
  channel.trim().toLowerCase() === "dev"
    ? DEFAULT_DEV_UPDATE_MANIFEST_URL
    : DEFAULT_UPDATE_MANIFEST_URL;

const DEFAULT_MANIFEST_URLS = new Set([
  DEFAULT_UPDATE_MANIFEST_URL,
  DEFAULT_DEV_UPDATE_MANIFEST_URL,
]);

const LLM_PROVIDER_DEFAULTS = {
  "openai-compatible": {
    baseURL: "https://api.deepseek.com/v1",
    model: "deepseek-chat",
  },
  "claude-messages": {
    baseURL: "https://api.anthropic.com/v1",
    model: "claude-sonnet-4-5",
  },
} satisfies Record<
  LocalConfig["llm"]["apiType"],
  { baseURL: string; model: string }
>;

export function Settings() {
  const qc = useQueryClient();
  const { user, loading: authLoading } = useAuth();
  const isAdmin = user?.role === "admin";
  const configKey = ["config", "admin", user?.id] as const;
  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: configKey,
    queryFn: ({ signal }) => api.getConfig(signal),
    enabled: isAdmin,
  });

  const [form, setForm] = useState<LocalConfig | null>(null);
  const [apiKeyDirty, setApiKeyDirty] = useState(false);
  const [cookieDirty, setCookieDirty] = useState(false);
  const [testResult, setTestResult] = useState<{
    ok: boolean;
    msg: string;
  } | null>(null);
  const [validationError, setValidationError] = useState<string | null>(null);

  useEffect(() => {
    if (!data) return;
    setForm({
      server: {
        host: data.server.host,
        port: data.server.port,
        publicOrigin: data.server.publicOrigin,
      },
      auth: {
        sessionTtlDays: data.auth.sessionTtlDays,
      },
      stream: {
        heartbeatMs: data.stream.heartbeatMs,
      },
      import: {
        minHtmlLength: data.import.minHtmlLength,
      },
      ui: {
        libraryRefetchIntervalMs: data.ui.libraryRefetchIntervalMs,
      },
      llm: {
        apiType: data.llm.apiType,
        baseURL: data.llm.baseURL,
        apiKey: data.llm.apiKey,
        model: data.llm.model,
        temperature: data.llm.temperature,
        concurrency: data.llm.concurrency,
        blocksPerRequest: data.llm.blocksPerRequest,
        maxTokensPerRequest: data.llm.maxTokensPerRequest,
        maxAutoRetries: data.llm.maxAutoRetries,
        mode: data.llm.mode ?? "normal",
        analysisMaxInputTokens: data.llm.analysisMaxInputTokens ?? 60000,
        stream: data.llm.stream ?? false,
      },
      ao3: { cookie: data.ao3.cookie, userAgent: data.ao3.userAgent },
      reader: {
        defaultMeasure: data.reader.defaultMeasure,
        defaultFont: data.reader.defaultFont,
        defaultZhScale: data.reader.defaultZhScale,
      },
      update: {
        manifestURL: data.update.manifestURL,
        channel: data.update.channel,
        autoCheck: data.update.autoCheck,
        restartDelayMs: data.update.restartDelayMs,
      },
    });
  }, [data]);

  const save = useMutation({
    mutationFn: (body: ConfigUpdate) => api.saveConfig(body),
    onSuccess: async () => {
      setApiKeyDirty(false);
      setCookieDirty(false);
      setForm((current) =>
        current
          ? {
              ...current,
              llm: { ...current.llm, apiKey: "" },
              ao3: { ...current.ao3, cookie: "" },
            }
          : current,
      );
      await qc.invalidateQueries({ queryKey: configKey });
    },
  });

  const test = useMutation({
    mutationFn: () => api.testConfig(),
    onSuccess: (r) => {
      setTestResult(
        r.ok
          ? { ok: true, msg: `通过：${(r.content ?? "").slice(0, 80)}` }
          : { ok: false, msg: r.error ?? "失败" },
      );
    },
    onError: (error) => {
      setTestResult({
        ok: false,
        msg: error instanceof Error ? error.message : "测试失败",
      });
    },
  });

  if (authLoading) return <SettingsSkeleton />;
  if (!isAdmin) {
    return (
      <Alert variant="destructive">
        <Settings2 />
        <AlertTitle>需要管理员权限</AlertTitle>
        <AlertDescription>当前账号无法访问服务配置。</AlertDescription>
      </Alert>
    );
  }
  if (isError) {
    return (
      <Alert variant="destructive">
        <ServerCog />
        <AlertTitle>配置加载失败</AlertTitle>
        <AlertDescription>
          {error.message}
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            重试
          </Button>
        </AlertDescription>
      </Alert>
    );
  }
  if (isLoading || !form) return <SettingsSkeleton />;

  const onSave = () => {
    save.reset();
    const parsed = ConfigSchema.safeParse(form);
    if (!parsed.success) {
      const issue = parsed.error.issues[0];
      const field = issue?.path.join(".");
      setValidationError(
        `${field ? `${field}：` : ""}${issue?.message ?? "配置无效"}`,
      );
      return;
    }
    const valid = parsed.data;
    const body: ConfigUpdate = {
      server: valid.server,
      auth: valid.auth,
      stream: valid.stream,
      import: valid.import,
      ui: valid.ui,
      llm: {
        apiType: valid.llm.apiType,
        baseURL: valid.llm.baseURL,
        model: valid.llm.model,
        temperature: valid.llm.temperature,
        concurrency: valid.llm.concurrency,
        blocksPerRequest: valid.llm.blocksPerRequest,
        maxTokensPerRequest: valid.llm.maxTokensPerRequest,
        maxAutoRetries: valid.llm.maxAutoRetries,
        mode: valid.llm.mode,
        analysisMaxInputTokens: valid.llm.analysisMaxInputTokens,
        stream: valid.llm.stream,
        ...(apiKeyDirty ? { apiKey: valid.llm.apiKey } : {}),
      },
      ao3: {
        userAgent: valid.ao3.userAgent,
        ...(cookieDirty ? { cookie: valid.ao3.cookie } : {}),
      },
      reader: valid.reader,
      update: valid.update,
    };
    setValidationError(null);
    setTestResult(null);
    save.mutate(body);
  };

  const setUpdateChannel = (channel: string) => {
    setForm((current) => {
      if (!current) return current;
      const manifestURL = current.update.manifestURL.trim();
      const nextURL =
        !manifestURL || DEFAULT_MANIFEST_URLS.has(manifestURL)
          ? defaultManifestURLForChannel(channel)
          : current.update.manifestURL;
      return {
        ...current,
        update: {
          ...current.update,
          channel,
          manifestURL: nextURL,
        },
      };
    });
  };

  const setLlmAPIType = (apiType: LocalConfig["llm"]["apiType"]) => {
    setForm((current) => {
      if (!current) return current;
      const previousDefaults = LLM_PROVIDER_DEFAULTS[current.llm.apiType];
      const nextDefaults = LLM_PROVIDER_DEFAULTS[apiType];
      return {
        ...current,
        llm: {
          ...current.llm,
          apiType,
          baseURL:
            current.llm.baseURL.trim() === previousDefaults.baseURL
              ? nextDefaults.baseURL
              : current.llm.baseURL,
          model:
            current.llm.model.trim() === previousDefaults.model
              ? nextDefaults.model
              : current.llm.model,
        },
      };
    });
  };

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-8 fade-in">
      <header className="flex flex-col gap-2">
        <Badge variant="accent">
          <Settings2 />
          System configuration
        </Badge>
        <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">服务设置</h1>
        <p className="max-w-3xl text-sm leading-relaxed text-muted-foreground">
          配置服务监听、LLM Provider、AO3 凭据、阅读器默认值与 OTA 更新。配置保存在服务端 data/config.json。
        </p>
      </header>

      <fieldset
        disabled={save.isPending}
        data-disabled={save.isPending || undefined}
        className="contents"
      >
      <SettingsCard title="服务与会话" description="监听地址、反代公开域名、会话时长和后台刷新频率。">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="server-host" label="Host">
            <Input
              id="server-host"
              value={form.server.host}
              onChange={(e) =>
                setForm({
                  ...form,
                  server: { ...form.server, host: e.target.value },
                })
              }
            />
          </Field>
          <Field id="server-port" label="Port（重启后生效）">
            <Input
              id="server-port"
              type="number"
              min={CONFIG_LIMITS.server.port.min}
              max={CONFIG_LIMITS.server.port.max}
              value={form.server.port}
              onChange={(e) =>
                setForm({
                  ...form,
                  server: { ...form.server, port: Number(e.target.value) },
                })
              }
            />
          </Field>
        </div>
        <Field
          id="server-public-origin"
          label="Public origin（反代域名，重启后生效）"
        >
          <Input
            id="server-public-origin"
            placeholder="https://ao3hub.example.com"
            value={form.server.publicOrigin}
            onChange={(e) =>
              setForm({
                ...form,
                server: { ...form.server, publicOrigin: e.target.value },
              })
            }
          />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="auth-session-ttl" label="Session TTL days">
            <Input
              id="auth-session-ttl"
              type="number"
              min={CONFIG_LIMITS.auth.sessionTtlDays.min}
              max={CONFIG_LIMITS.auth.sessionTtlDays.max}
              value={form.auth.sessionTtlDays}
              onChange={(e) =>
                setForm({
                  ...form,
                  auth: {
                    ...form.auth,
                    sessionTtlDays: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
          <Field id="stream-heartbeat" label="SSE heartbeat ms">
            <Input
              id="stream-heartbeat"
              type="number"
              min={CONFIG_LIMITS.stream.heartbeatMs.min}
              max={CONFIG_LIMITS.stream.heartbeatMs.max}
              value={form.stream.heartbeatMs}
              onChange={(e) =>
                setForm({
                  ...form,
                  stream: {
                    ...form.stream,
                    heartbeatMs: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="import-min-html" label="Min HTML length">
            <Input
              id="import-min-html"
              type="number"
              min={CONFIG_LIMITS.import.minHtmlLength.min}
              max={CONFIG_LIMITS.import.minHtmlLength.max}
              value={form.import.minHtmlLength}
              onChange={(e) =>
                setForm({
                  ...form,
                  import: {
                    ...form.import,
                    minHtmlLength: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
          <Field id="ui-library-refetch" label="Library refresh ms">
            <Input
              id="ui-library-refetch"
              type="number"
              min={CONFIG_LIMITS.ui.libraryRefetchIntervalMs.min}
              max={CONFIG_LIMITS.ui.libraryRefetchIntervalMs.max}
              value={form.ui.libraryRefetchIntervalMs}
              onChange={(e) =>
                setForm({
                  ...form,
                  ui: {
                    ...form.ui,
                    libraryRefetchIntervalMs: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
        </div>
      </SettingsCard>

      <SettingsCard title="LLM Provider" description="模型端点、请求策略与默认翻译模式。">
        <Field id="llm-api-type" label="API Type">
          <ToggleGroup
            id="llm-api-type"
            type="single"
            value={form.llm.apiType}
            onValueChange={(value) =>
              value && setLlmAPIType(value as LocalConfig["llm"]["apiType"])
            }
            variant="outline"
            className="flex-wrap"
          >
            {[
              ["openai-compatible", "OpenAI compatible"],
              ["claude-messages", "Claude Messages"],
            ].map(([apiType, label]) => (
              <ToggleGroupItem
                key={apiType}
                value={apiType}
              >
                {label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </Field>
        <Field id="llm-baseurl" label="Base URL">
          <Input
            id="llm-baseurl"
            placeholder={
              form.llm.apiType === "claude-messages"
                ? "https://api.anthropic.com/v1"
                : "https://api.deepseek.com/v1"
            }
            value={form.llm.baseURL}
            onChange={(e) =>
              setForm({
                ...form,
                llm: { ...form.llm, baseURL: e.target.value },
              })
            }
          />
        </Field>
        <Field
          id="llm-apikey"
          label={`API Key${data?.llm.hasApiKey ? "（已配置，不修改则保留）" : ""}`}
        >
          <Input
            id="llm-apikey"
            type="password"
            placeholder={data?.llm.hasApiKey ? "已存在 — 输入新值替换" : "sk-…"}
            value={apiKeyDirty ? form.llm.apiKey : ""}
            onChange={(e) => {
              setApiKeyDirty(true);
              setForm({
                ...form,
                llm: { ...form.llm, apiKey: e.target.value },
              });
            }}
          />
        </Field>
        <Field id="llm-model" label="Model">
          <Input
            id="llm-model"
            value={form.llm.model}
            onChange={(e) =>
              setForm({ ...form, llm: { ...form.llm, model: e.target.value } })
            }
          />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Field id="llm-temp" label="Temperature">
            <Input
              id="llm-temp"
              type="number"
              min={CONFIG_LIMITS.llm.temperature.min}
              max={CONFIG_LIMITS.llm.temperature.max}
              step={CONFIG_LIMITS.llm.temperature.step}
              value={form.llm.temperature}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: { ...form.llm, temperature: Number(e.target.value) },
                })
              }
            />
          </Field>
          <Field id="llm-conc" label="Concurrency">
            <Input
              id="llm-conc"
              type="number"
              min={CONFIG_LIMITS.llm.concurrency.min}
              value={form.llm.concurrency}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: { ...form.llm, concurrency: Number(e.target.value) },
                })
              }
            />
          </Field>
          <Field id="llm-blocks" label="Blocks / request">
            <Input
              id="llm-blocks"
              type="number"
              min={CONFIG_LIMITS.llm.blocksPerRequest.min}
              max={CONFIG_LIMITS.llm.blocksPerRequest.max}
              value={form.llm.blocksPerRequest}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: {
                    ...form.llm,
                    blocksPerRequest: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
          <Field id="llm-max-tokens" label="Max tokens">
            <Input
              id="llm-max-tokens"
              type="number"
              min={CONFIG_LIMITS.llm.maxTokensPerRequest.min}
              max={CONFIG_LIMITS.llm.maxTokensPerRequest.max}
              value={form.llm.maxTokensPerRequest}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: {
                    ...form.llm,
                    maxTokensPerRequest: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="llm-auto-retries" label="自动重试轮次">
            <Input
              id="llm-auto-retries"
              type="number"
              min={CONFIG_LIMITS.llm.maxAutoRetries.min}
              max={CONFIG_LIMITS.llm.maxAutoRetries.max}
              value={form.llm.maxAutoRetries}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: { ...form.llm, maxAutoRetries: Number(e.target.value) },
                })
              }
            />
          </Field>
          <FieldPrimitive orientation="horizontal" className="self-end pb-2">
            <Switch
              id="llm-stream"
              checked={form.llm.stream}
              onCheckedChange={(v) =>
                setForm({
                  ...form,
                  llm: { ...form.llm, stream: v },
                })
              }
            />
            <FieldLabel htmlFor="llm-stream">
              启用流式 LLM 请求（SSE 透明接收，减少长请求超时）
            </FieldLabel>
          </FieldPrimitive>
        </div>
        <Field id="llm-mode" label="翻译模式（默认）">
          <ToggleGroup
            id="llm-mode"
            type="single"
            value={form.llm.mode}
            onValueChange={(value) =>
              value && setForm({
                ...form,
                llm: { ...form.llm, mode: value as LocalConfig["llm"]["mode"] },
              })
            }
            variant="outline"
            className="flex-wrap"
          >
            {[
              ["normal", "普通"],
              ["refined", "精翻（AO3 同人专家 + 全文预读）"],
            ].map(([mode, label]) => (
              <ToggleGroupItem
                key={mode}
                value={mode}
              >
                {label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </Field>
        <Field
          id="llm-analysis-tokens"
          label="精翻：单次预读 token 上限（超过则自动降级为分章 + 归并）"
        >
          <Input
            id="llm-analysis-tokens"
            type="number"
            min={CONFIG_LIMITS.llm.analysisMaxInputTokens.min}
            max={CONFIG_LIMITS.llm.analysisMaxInputTokens.max}
            value={form.llm.analysisMaxInputTokens}
            onChange={(e) =>
              setForm({
                ...form,
                llm: {
                  ...form.llm,
                  analysisMaxInputTokens: Number(e.target.value),
                },
              })
            }
          />
        </Field>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="outline"
            onClick={() => test.mutate()}
            disabled={test.isPending}
          >
            {test.isPending && <Spinner data-icon="inline-start" />}
            {test.isPending ? "测试中" : "测试连通"}
          </Button>
          {testResult && (
            <Badge variant={testResult.ok ? "success" : "destructive"}>
              {testResult.ok ? <Check /> : <X />}
              {testResult.msg}
            </Badge>
          )}
        </div>
      </SettingsCard>

      <SettingsCard title="AO3 访问" description="用于抓取受限作品的 Cookie 与 User Agent。">
        <Field
          id="ao3-cookie"
          label={`Cookie${data?.ao3.hasCookie ? "（已配置，不修改则保留）" : ""}`}
        >
          <Textarea
            id="ao3-cookie"
            rows={3}
            placeholder={
              data?.ao3.hasCookie
                ? "已存在 — 输入新值替换"
                : "_otwarchive_session=…"
            }
            value={cookieDirty ? form.ao3.cookie : ""}
            onChange={(e) => {
              setCookieDirty(true);
              setForm({
                ...form,
                ao3: { ...form.ao3, cookie: e.target.value },
              });
            }}
          />
        </Field>
        <Field id="ao3-ua" label="User Agent">
          <Input
            id="ao3-ua"
            value={form.ao3.userAgent}
            onChange={(e) =>
              setForm({
                ...form,
                ao3: { ...form.ao3, userAgent: e.target.value },
              })
            }
          />
        </Field>
      </SettingsCard>

      <SettingsCard title="阅读器默认值" description="新设备首次打开阅读器时使用的排版参数。">
        <div className="grid gap-4 sm:grid-cols-3">
          <Field id="reader-font" label="Font px">
            <Input
              id="reader-font"
              type="number"
              min={CONFIG_LIMITS.reader.defaultFont.min}
              max={CONFIG_LIMITS.reader.defaultFont.max}
              step={CONFIG_LIMITS.reader.defaultFont.step}
              value={form.reader.defaultFont}
              onChange={(e) =>
                setForm({
                  ...form,
                  reader: {
                    ...form.reader,
                    defaultFont: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
          <Field id="reader-zh-scale" label="ZH scale">
            <Input
              id="reader-zh-scale"
              type="number"
              min={CONFIG_LIMITS.reader.defaultZhScale.min}
              max={CONFIG_LIMITS.reader.defaultZhScale.max}
              step={CONFIG_LIMITS.reader.defaultZhScale.step}
              value={form.reader.defaultZhScale}
              onChange={(e) =>
                setForm({
                  ...form,
                  reader: {
                    ...form.reader,
                    defaultZhScale: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
          <Field id="reader-measure" label="Measure px">
            <Input
              id="reader-measure"
              type="number"
              min={CONFIG_LIMITS.reader.defaultMeasure.min}
              max={CONFIG_LIMITS.reader.defaultMeasure.max}
              step={CONFIG_LIMITS.reader.defaultMeasure.step}
              value={form.reader.defaultMeasure}
              onChange={(e) =>
                setForm({
                  ...form,
                  reader: {
                    ...form.reader,
                    defaultMeasure: Number(e.target.value),
                  },
                })
              }
            />
          </Field>
        </div>
      </SettingsCard>

      <SettingsCard title="OTA 更新" description="发行通道、Manifest 地址与重启策略。">
        <Field id="ota-manifest" label="Manifest URL">
          <Input
            id="ota-manifest"
            placeholder={defaultManifestURLForChannel(form.update.channel)}
            value={form.update.manifestURL}
            onChange={(e) =>
              setForm({
                ...form,
                update: { ...form.update, manifestURL: e.target.value },
              })
            }
          />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="ota-channel" label="Channel">
            <ToggleGroup
              id="ota-channel"
              type="single"
              value={form.update.channel}
              onValueChange={(value) => value && setUpdateChannel(value)}
              variant="outline"
            >
              {["stable", "dev"].map((channel) => (
                <ToggleGroupItem
                  key={channel}
                  value={channel}
                >
                  {channel}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </Field>
          <FieldPrimitive orientation="horizontal" className="self-end pb-2">
            <Switch
              id="ota-auto"
              checked={form.update.autoCheck}
              onCheckedChange={(v) =>
                setForm({
                  ...form,
                  update: { ...form.update, autoCheck: v },
                })
              }
            />
            <FieldLabel htmlFor="ota-auto">
              启动时自动检查更新
            </FieldLabel>
          </FieldPrimitive>
        </div>
        <Field id="ota-restart-delay" label="Restart delay ms">
          <Input
            id="ota-restart-delay"
            type="number"
            min={CONFIG_LIMITS.update.restartDelayMs.min}
            max={CONFIG_LIMITS.update.restartDelayMs.max}
            value={form.update.restartDelayMs}
            onChange={(e) =>
              setForm({
                ...form,
                update: {
                  ...form.update,
                  restartDelayMs: Number(e.target.value),
                },
              })
            }
          />
        </Field>
      </SettingsCard>
      </fieldset>

      <Separator />

      {(save.isError || validationError) && (
        <Alert variant="destructive">
          <Settings2 />
          <AlertTitle>无法保存配置</AlertTitle>
          <AlertDescription>
            {validationError ||
              (save.error instanceof Error ? save.error.message : "保存失败")}
          </AlertDescription>
        </Alert>
      )}

      <div className="sticky bottom-4 flex flex-wrap items-center gap-3 rounded-xl border bg-background/90 p-3 shadow-float backdrop-blur-xl">
        <Button
          onClick={onSave}
          disabled={save.isPending || test.isPending}
        >
          {save.isPending && <Spinner data-icon="inline-start" />}
          {save.isPending ? "保存中" : "保存全部设置"}
        </Button>
        {save.isSuccess && (
          <Badge variant="success">
            <Check />
            已保存
          </Badge>
        )}
        <span className="ml-auto text-xs text-muted-foreground">
          API Key 与 Cookie 留空时会保留现有值
        </span>
      </div>
    </div>
  );
}

function Field({
  id,
  label,
  children,
}: {
  id: string;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <FieldPrimitive>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {children}
    </FieldPrimitive>
  );
}

function SettingsCard({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">{children}</CardContent>
    </Card>
  );
}

function SettingsSkeleton() {
  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-5 w-3/4" />
      </div>
      {Array.from({ length: 3 }).map((_, index) => (
        <Card key={index}>
          <CardHeader>
            <Skeleton className="h-6 w-36" />
            <Skeleton className="h-4 w-2/3" />
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
