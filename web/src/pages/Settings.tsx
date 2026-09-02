import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  DEFAULT_DEV_UPDATE_MANIFEST_URL,
  DEFAULT_UPDATE_MANIFEST_URL,
  CONFIG_LIMITS,
  Config as ConfigSchema,
  type Config,
} from "@ao3hub/shared";
import {
  BookOpen,
  Check,
  Cpu,
  KeyRound,
  PackageCheck,
  ServerCog,
  Settings2,
  Undo2,
  X,
} from "lucide-react";
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
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
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

/** Strip the write-only secrets so the dirty check compares like with like. */
function comparable(config: LocalConfig): string {
  return JSON.stringify({
    ...config,
    llm: { ...config.llm, apiKey: "" },
    ao3: { ...config.ao3, cookie: "" },
  });
}

function formFromServer(data: Config): LocalConfig {
  return {
    server: {
      host: data.server.host,
      port: data.server.port,
      publicOrigin: data.server.publicOrigin,
    },
    auth: { sessionTtlDays: data.auth.sessionTtlDays },
    stream: { heartbeatMs: data.stream.heartbeatMs },
    import: { minHtmlLength: data.import.minHtmlLength },
    ui: { libraryRefetchIntervalMs: data.ui.libraryRefetchIntervalMs },
    llm: {
      apiType: data.llm.apiType,
      baseURL: data.llm.baseURL,
      apiKey: "",
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
    ao3: { cookie: "", userAgent: data.ao3.userAgent },
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
  };
}

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
  const [baseline, setBaseline] = useState<string | null>(null);
  const [apiKeyDirty, setApiKeyDirty] = useState(false);
  const [cookieDirty, setCookieDirty] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(
    null,
  );
  const [validationError, setValidationError] = useState<string | null>(null);

  useEffect(() => {
    if (!data) return;
    const next = formFromServer(data);
    setForm(next);
    setBaseline(comparable(next));
    setApiKeyDirty(false);
    setCookieDirty(false);
  }, [data]);

  const dirty = useMemo(() => {
    if (!form || baseline === null) return false;
    return apiKeyDirty || cookieDirty || comparable(form) !== baseline;
  }, [form, baseline, apiKeyDirty, cookieDirty]);

  // Browsers warn on close/reload while a config edit is pending.
  useEffect(() => {
    if (!dirty) return;
    const onBeforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [dirty]);

  const save = useMutation({
    mutationFn: (body: ConfigUpdate) => api.saveConfig(body),
    onSuccess: async () => {
      toast.success("配置已保存", {
        description: "端口与反代域名等选项需要重启后生效。",
      });
      await qc.invalidateQueries({ queryKey: configKey });
    },
    onError: (saveError) =>
      toast.error("保存失败", {
        description:
          saveError instanceof Error ? saveError.message : "未知错误",
      }),
  });

  const test = useMutation({
    mutationFn: () => api.testConfig(),
    onSuccess: (r) => {
      if (r.ok) {
        const preview = (r.content ?? "").slice(0, 80);
        setTestResult({ ok: true, msg: preview || "连接成功" });
        toast.success("LLM 连通性正常");
      } else {
        setTestResult({ ok: false, msg: r.error ?? "失败" });
        toast.error("LLM 连通性测试失败", { description: r.error });
      }
    },
    onError: (testError) => {
      const msg = testError instanceof Error ? testError.message : "测试失败";
      setTestResult({ ok: false, msg });
      toast.error("LLM 连通性测试失败", { description: msg });
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
        <AlertDescription className="flex flex-col items-start gap-3">
          {error.message}
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            重试
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  if (isLoading || !form) return <SettingsSkeleton />;

  const patch = (updater: (current: LocalConfig) => LocalConfig) =>
    setForm((current) => (current ? updater(current) : current));

  const onSave = () => {
    save.reset();
    const parsed = ConfigSchema.safeParse(form);
    if (!parsed.success) {
      const issue = parsed.error.issues[0];
      const field = issue?.path.join(".");
      const message = `${field ? `${field}：` : ""}${issue?.message ?? "配置无效"}`;
      setValidationError(message);
      toast.error("配置无效", { description: message });
      return;
    }
    const valid = parsed.data;
    setValidationError(null);
    setTestResult(null);
    save.mutate({
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
    });
  };

  const onDiscard = () => {
    if (!data) return;
    const next = formFromServer(data);
    setForm(next);
    setBaseline(comparable(next));
    setApiKeyDirty(false);
    setCookieDirty(false);
    setValidationError(null);
    toast.info("已放弃未保存的修改");
  };

  const setUpdateChannel = (channel: string) =>
    patch((current) => {
      const manifestURL = current.update.manifestURL.trim();
      const nextURL =
        !manifestURL || DEFAULT_MANIFEST_URLS.has(manifestURL)
          ? defaultManifestURLForChannel(channel)
          : current.update.manifestURL;
      return {
        ...current,
        update: { ...current.update, channel, manifestURL: nextURL },
      };
    });

  const setLlmAPIType = (apiType: LocalConfig["llm"]["apiType"]) =>
    patch((current) => {
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

  return (
    <div className="fade-in mx-auto flex w-full max-w-4xl flex-col gap-6">
      <header className="flex flex-col gap-2">
        <h1 className="cn-font-heading text-2xl font-semibold tracking-tight sm:text-3xl">
          服务设置
        </h1>
        <p className="max-w-3xl text-sm text-muted-foreground">
          配置服务监听、LLM Provider、AO3 凭据、阅读器默认值与 OTA 更新。所有配置保存在服务端的{" "}
          <code className="rounded bg-muted px-1 py-0.5 text-xs">data/config.json</code>。
        </p>
      </header>

      <fieldset disabled={save.isPending} className="contents">
        <Tabs defaultValue="server">
          <TabsList className="w-full overflow-x-auto">
            <TabsTrigger value="server">
              <ServerCog data-icon="inline-start" />
              服务
            </TabsTrigger>
            <TabsTrigger value="llm">
              <Cpu data-icon="inline-start" />
              LLM
            </TabsTrigger>
            <TabsTrigger value="ao3">
              <KeyRound data-icon="inline-start" />
              AO3
            </TabsTrigger>
            <TabsTrigger value="reader">
              <BookOpen data-icon="inline-start" />
              阅读器
            </TabsTrigger>
            <TabsTrigger value="update">
              <PackageCheck data-icon="inline-start" />
              更新
            </TabsTrigger>
          </TabsList>

          <TabsContent value="server" className="flex flex-col gap-5">
            <SettingsCard
              title="监听与反代"
              description="改动端口或公开域名后需要重启服务才会生效。"
            >
              <FieldGroup>
                <div className="grid gap-4 sm:grid-cols-2">
                  <SettingField
                    id="server-host"
                    label="Host"
                    description="新安装默认只监听 127.0.0.1。"
                  >
                    <Input
                      id="server-host"
                      value={form.server.host}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          server: { ...c.server, host: e.target.value },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField
                    id="server-port"
                    label="Port"
                    description={`${CONFIG_LIMITS.server.port.min}–${CONFIG_LIMITS.server.port.max}，重启后生效。`}
                  >
                    <Input
                      id="server-port"
                      type="number"
                      min={CONFIG_LIMITS.server.port.min}
                      max={CONFIG_LIMITS.server.port.max}
                      value={form.server.port}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          server: { ...c.server, port: Number(e.target.value) },
                        }))
                      }
                    />
                  </SettingField>
                </div>
                <SettingField
                  id="server-public-origin"
                  label="Public origin"
                  description="放在反向代理后面时填写对外域名，重启后生效。"
                >
                  <Input
                    id="server-public-origin"
                    placeholder="https://ao3hub.example.com"
                    value={form.server.publicOrigin}
                    onChange={(e) =>
                      patch((c) => ({
                        ...c,
                        server: { ...c.server, publicOrigin: e.target.value },
                      }))
                    }
                  />
                </SettingField>
              </FieldGroup>
            </SettingsCard>

            <SettingsCard
              title="会话与刷新"
              description="登录有效期、SSE 心跳与书架轮询节奏。"
            >
              <FieldGroup>
                <div className="grid gap-4 sm:grid-cols-2">
                  <SettingField
                    id="auth-session-ttl"
                    label="Session TTL（天）"
                    description={`${CONFIG_LIMITS.auth.sessionTtlDays.min}–${CONFIG_LIMITS.auth.sessionTtlDays.max}`}
                  >
                    <Input
                      id="auth-session-ttl"
                      type="number"
                      min={CONFIG_LIMITS.auth.sessionTtlDays.min}
                      max={CONFIG_LIMITS.auth.sessionTtlDays.max}
                      value={form.auth.sessionTtlDays}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          auth: { ...c.auth, sessionTtlDays: Number(e.target.value) },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField
                    id="stream-heartbeat"
                    label="SSE heartbeat（ms）"
                    description={`${CONFIG_LIMITS.stream.heartbeatMs.min}–${CONFIG_LIMITS.stream.heartbeatMs.max}`}
                  >
                    <Input
                      id="stream-heartbeat"
                      type="number"
                      min={CONFIG_LIMITS.stream.heartbeatMs.min}
                      max={CONFIG_LIMITS.stream.heartbeatMs.max}
                      value={form.stream.heartbeatMs}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          stream: { ...c.stream, heartbeatMs: Number(e.target.value) },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField
                    id="import-min-html"
                    label="最小 HTML 长度"
                    description="低于这个字节数的上传会被拒绝。"
                  >
                    <Input
                      id="import-min-html"
                      type="number"
                      min={CONFIG_LIMITS.import.minHtmlLength.min}
                      max={CONFIG_LIMITS.import.minHtmlLength.max}
                      value={form.import.minHtmlLength}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          import: {
                            ...c.import,
                            minHtmlLength: Number(e.target.value),
                          },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField
                    id="ui-library-refetch"
                    label="书架刷新间隔（ms）"
                    description="有作品在处理时的轮询频率。"
                  >
                    <Input
                      id="ui-library-refetch"
                      type="number"
                      min={CONFIG_LIMITS.ui.libraryRefetchIntervalMs.min}
                      max={CONFIG_LIMITS.ui.libraryRefetchIntervalMs.max}
                      value={form.ui.libraryRefetchIntervalMs}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          ui: {
                            ...c.ui,
                            libraryRefetchIntervalMs: Number(e.target.value),
                          },
                        }))
                      }
                    />
                  </SettingField>
                </div>
              </FieldGroup>
            </SettingsCard>
          </TabsContent>

          <TabsContent value="llm" className="flex flex-col gap-5">
            <SettingsCard title="Provider" description="模型端点与凭据。">
              <FieldGroup>
                <FieldSet>
                  <FieldLegend variant="label">API 类型</FieldLegend>
                  <ToggleGroup
                    type="single"
                    value={form.llm.apiType}
                    onValueChange={(value) =>
                      value && setLlmAPIType(value as LocalConfig["llm"]["apiType"])
                    }
                    variant="outline"
                    className="flex-wrap"
                  >
                    <ToggleGroupItem value="openai-compatible">
                      OpenAI compatible
                    </ToggleGroupItem>
                    <ToggleGroupItem value="claude-messages">
                      Claude Messages
                    </ToggleGroupItem>
                  </ToggleGroup>
                </FieldSet>
                <SettingField id="llm-baseurl" label="Base URL">
                  <Input
                    id="llm-baseurl"
                    placeholder={LLM_PROVIDER_DEFAULTS[form.llm.apiType].baseURL}
                    value={form.llm.baseURL}
                    onChange={(e) =>
                      patch((c) => ({
                        ...c,
                        llm: { ...c.llm, baseURL: e.target.value },
                      }))
                    }
                  />
                </SettingField>
                <SettingField
                  id="llm-apikey"
                  label="API Key"
                  description={
                    data?.llm.hasApiKey
                      ? "已配置。留空则保留现有值，输入新值会覆盖。"
                      : "尚未配置，翻译前需要填写。"
                  }
                >
                  <Input
                    id="llm-apikey"
                    type="password"
                    autoComplete="off"
                    placeholder={data?.llm.hasApiKey ? "••••••••" : "sk-…"}
                    value={apiKeyDirty ? form.llm.apiKey : ""}
                    onChange={(e) => {
                      setApiKeyDirty(true);
                      patch((c) => ({
                        ...c,
                        llm: { ...c.llm, apiKey: e.target.value },
                      }));
                    }}
                  />
                </SettingField>
                <SettingField id="llm-model" label="Model">
                  <Input
                    id="llm-model"
                    placeholder={LLM_PROVIDER_DEFAULTS[form.llm.apiType].model}
                    value={form.llm.model}
                    onChange={(e) =>
                      patch((c) => ({
                        ...c,
                        llm: { ...c.llm, model: e.target.value },
                      }))
                    }
                  />
                </SettingField>
                <Field orientation="horizontal">
                  <Button
                    type="button"
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
                      <span className="max-w-[22rem] truncate">
                        {testResult.msg}
                      </span>
                    </Badge>
                  )}
                </Field>
              </FieldGroup>
            </SettingsCard>

            <SettingsCard
              title="请求策略"
              description="批量大小、并发与重试。数值越大越快，也越容易触发上游限流。"
            >
              <FieldGroup>
                <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                  <SettingField id="llm-temp" label="Temperature">
                    <Input
                      id="llm-temp"
                      type="number"
                      min={CONFIG_LIMITS.llm.temperature.min}
                      max={CONFIG_LIMITS.llm.temperature.max}
                      step={CONFIG_LIMITS.llm.temperature.step}
                      value={form.llm.temperature}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          llm: { ...c.llm, temperature: Number(e.target.value) },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField id="llm-conc" label="Concurrency">
                    <Input
                      id="llm-conc"
                      type="number"
                      min={CONFIG_LIMITS.llm.concurrency.min}
                      value={form.llm.concurrency}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          llm: { ...c.llm, concurrency: Number(e.target.value) },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField id="llm-blocks" label="Blocks / request">
                    <Input
                      id="llm-blocks"
                      type="number"
                      min={CONFIG_LIMITS.llm.blocksPerRequest.min}
                      max={CONFIG_LIMITS.llm.blocksPerRequest.max}
                      value={form.llm.blocksPerRequest}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          llm: {
                            ...c.llm,
                            blocksPerRequest: Number(e.target.value),
                          },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField id="llm-max-tokens" label="Max tokens">
                    <Input
                      id="llm-max-tokens"
                      type="number"
                      min={CONFIG_LIMITS.llm.maxTokensPerRequest.min}
                      max={CONFIG_LIMITS.llm.maxTokensPerRequest.max}
                      value={form.llm.maxTokensPerRequest}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          llm: {
                            ...c.llm,
                            maxTokensPerRequest: Number(e.target.value),
                          },
                        }))
                      }
                    />
                  </SettingField>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <SettingField
                    id="llm-auto-retries"
                    label="自动重试轮次"
                    description={`${CONFIG_LIMITS.llm.maxAutoRetries.min}–${CONFIG_LIMITS.llm.maxAutoRetries.max}`}
                  >
                    <Input
                      id="llm-auto-retries"
                      type="number"
                      min={CONFIG_LIMITS.llm.maxAutoRetries.min}
                      max={CONFIG_LIMITS.llm.maxAutoRetries.max}
                      value={form.llm.maxAutoRetries}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          llm: { ...c.llm, maxAutoRetries: Number(e.target.value) },
                        }))
                      }
                    />
                  </SettingField>
                  <Field
                    orientation="horizontal"
                    className="self-start rounded-lg border p-3"
                  >
                    <FieldLabel htmlFor="llm-stream" className="font-normal">
                      流式 LLM 请求
                      <FieldDescription>
                        以 SSE 接收响应，降低长请求超时的概率。
                      </FieldDescription>
                    </FieldLabel>
                    <Switch
                      id="llm-stream"
                      checked={form.llm.stream}
                      onCheckedChange={(v) =>
                        patch((c) => ({ ...c, llm: { ...c.llm, stream: v } }))
                      }
                    />
                  </Field>
                </div>
              </FieldGroup>
            </SettingsCard>

            <SettingsCard
              title="翻译模式"
              description="导入时可以逐篇覆盖，这里设置的是默认值。"
            >
              <FieldGroup>
                <FieldSet>
                  <FieldLegend variant="label">默认模式</FieldLegend>
                  <ToggleGroup
                    type="single"
                    value={form.llm.mode}
                    onValueChange={(value) =>
                      value &&
                      patch((c) => ({
                        ...c,
                        llm: { ...c.llm, mode: value as LocalConfig["llm"]["mode"] },
                      }))
                    }
                    variant="outline"
                    className="flex-wrap"
                  >
                    <ToggleGroupItem value="normal">普通</ToggleGroupItem>
                    <ToggleGroupItem value="refined">
                      精翻（全文预读 + 术语表）
                    </ToggleGroupItem>
                  </ToggleGroup>
                  <FieldDescription>
                    {form.llm.mode === "refined"
                      ? "先通读全文再翻译，质量更高，首次处理更慢、成本更高。"
                      : "直接分块翻译，速度快、成本低。"}
                  </FieldDescription>
                </FieldSet>
                <SettingField
                  id="llm-analysis-tokens"
                  label="精翻：单次预读 token 上限"
                  description="超出后自动降级为分章预读 + 归并。"
                >
                  <Input
                    id="llm-analysis-tokens"
                    type="number"
                    min={CONFIG_LIMITS.llm.analysisMaxInputTokens.min}
                    max={CONFIG_LIMITS.llm.analysisMaxInputTokens.max}
                    value={form.llm.analysisMaxInputTokens}
                    onChange={(e) =>
                      patch((c) => ({
                        ...c,
                        llm: {
                          ...c.llm,
                          analysisMaxInputTokens: Number(e.target.value),
                        },
                      }))
                    }
                  />
                </SettingField>
              </FieldGroup>
            </SettingsCard>
          </TabsContent>

          <TabsContent value="ao3" className="flex flex-col gap-5">
            <SettingsCard
              title="AO3 访问"
              description="抓取受限或 Explicit 作品时需要登录态 Cookie。"
            >
              <FieldGroup>
                <SettingField
                  id="ao3-cookie"
                  label="Cookie"
                  description={
                    data?.ao3.hasCookie
                      ? "已配置。留空则保留现有值，输入新值会覆盖。"
                      : "从浏览器 AO3 会话中复制 _otwarchive_session。"
                  }
                >
                  <Textarea
                    id="ao3-cookie"
                    rows={3}
                    autoComplete="off"
                    placeholder={
                      data?.ao3.hasCookie ? "••••••••" : "_otwarchive_session=…"
                    }
                    value={cookieDirty ? form.ao3.cookie : ""}
                    onChange={(e) => {
                      setCookieDirty(true);
                      patch((c) => ({
                        ...c,
                        ao3: { ...c.ao3, cookie: e.target.value },
                      }));
                    }}
                  />
                </SettingField>
                <SettingField
                  id="ao3-ua"
                  label="User Agent"
                  description="与提供 Cookie 的浏览器保持一致可降低被拦截的概率。"
                >
                  <Input
                    id="ao3-ua"
                    value={form.ao3.userAgent}
                    onChange={(e) =>
                      patch((c) => ({
                        ...c,
                        ao3: { ...c.ao3, userAgent: e.target.value },
                      }))
                    }
                  />
                </SettingField>
              </FieldGroup>
            </SettingsCard>
          </TabsContent>

          <TabsContent value="reader" className="flex flex-col gap-5">
            <SettingsCard
              title="阅读器默认值"
              description="新设备首次打开阅读器时使用的排版参数；之后以设备上的设置为准。"
            >
              <FieldGroup>
                <div className="grid gap-4 sm:grid-cols-3">
                  <SettingField
                    id="reader-font"
                    label="字号 (px)"
                    description={`${CONFIG_LIMITS.reader.defaultFont.min}–${CONFIG_LIMITS.reader.defaultFont.max}`}
                  >
                    <Input
                      id="reader-font"
                      type="number"
                      min={CONFIG_LIMITS.reader.defaultFont.min}
                      max={CONFIG_LIMITS.reader.defaultFont.max}
                      step={CONFIG_LIMITS.reader.defaultFont.step}
                      value={form.reader.defaultFont}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          reader: { ...c.reader, defaultFont: Number(e.target.value) },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField
                    id="reader-zh-scale"
                    label="中文比例"
                    description={`${CONFIG_LIMITS.reader.defaultZhScale.min}–${CONFIG_LIMITS.reader.defaultZhScale.max}`}
                  >
                    <Input
                      id="reader-zh-scale"
                      type="number"
                      min={CONFIG_LIMITS.reader.defaultZhScale.min}
                      max={CONFIG_LIMITS.reader.defaultZhScale.max}
                      step={CONFIG_LIMITS.reader.defaultZhScale.step}
                      value={form.reader.defaultZhScale}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          reader: {
                            ...c.reader,
                            defaultZhScale: Number(e.target.value),
                          },
                        }))
                      }
                    />
                  </SettingField>
                  <SettingField
                    id="reader-measure"
                    label="栏宽 (px)"
                    description={`${CONFIG_LIMITS.reader.defaultMeasure.min}–${CONFIG_LIMITS.reader.defaultMeasure.max}`}
                  >
                    <Input
                      id="reader-measure"
                      type="number"
                      min={CONFIG_LIMITS.reader.defaultMeasure.min}
                      max={CONFIG_LIMITS.reader.defaultMeasure.max}
                      step={CONFIG_LIMITS.reader.defaultMeasure.step}
                      value={form.reader.defaultMeasure}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          reader: {
                            ...c.reader,
                            defaultMeasure: Number(e.target.value),
                          },
                        }))
                      }
                    />
                  </SettingField>
                </div>
              </FieldGroup>
            </SettingsCard>
          </TabsContent>

          <TabsContent value="update" className="flex flex-col gap-5">
            <SettingsCard
              title="OTA 更新"
              description="发行通道、Manifest 地址与重启策略。更新包只做 sha256 校验。"
            >
              <FieldGroup>
                <FieldSet>
                  <FieldLegend variant="label">Channel</FieldLegend>
                  <ToggleGroup
                    type="single"
                    value={form.update.channel}
                    onValueChange={(value) => value && setUpdateChannel(value)}
                    variant="outline"
                  >
                    <ToggleGroupItem value="stable">stable</ToggleGroupItem>
                    <ToggleGroupItem value="dev">dev</ToggleGroupItem>
                  </ToggleGroup>
                  <FieldDescription>
                    切换通道会同步替换默认 Manifest 地址（自定义地址会保留）。
                  </FieldDescription>
                </FieldSet>
                <SettingField id="ota-manifest" label="Manifest URL">
                  <Input
                    id="ota-manifest"
                    placeholder={defaultManifestURLForChannel(form.update.channel)}
                    value={form.update.manifestURL}
                    onChange={(e) =>
                      patch((c) => ({
                        ...c,
                        update: { ...c.update, manifestURL: e.target.value },
                      }))
                    }
                  />
                </SettingField>
                <div className="grid gap-4 sm:grid-cols-2">
                  <SettingField
                    id="ota-restart-delay"
                    label="重启延迟（ms）"
                    description={`${CONFIG_LIMITS.update.restartDelayMs.min}–${CONFIG_LIMITS.update.restartDelayMs.max}`}
                  >
                    <Input
                      id="ota-restart-delay"
                      type="number"
                      min={CONFIG_LIMITS.update.restartDelayMs.min}
                      max={CONFIG_LIMITS.update.restartDelayMs.max}
                      value={form.update.restartDelayMs}
                      onChange={(e) =>
                        patch((c) => ({
                          ...c,
                          update: {
                            ...c.update,
                            restartDelayMs: Number(e.target.value),
                          },
                        }))
                      }
                    />
                  </SettingField>
                  <Field
                    orientation="horizontal"
                    className="self-start rounded-lg border p-3"
                  >
                    <FieldLabel htmlFor="ota-auto" className="font-normal">
                      启动时自动检查更新
                      <FieldDescription>
                        只检查，不会自动安装。
                      </FieldDescription>
                    </FieldLabel>
                    <Switch
                      id="ota-auto"
                      checked={form.update.autoCheck}
                      onCheckedChange={(v) =>
                        patch((c) => ({
                          ...c,
                          update: { ...c.update, autoCheck: v },
                        }))
                      }
                    />
                  </Field>
                </div>
              </FieldGroup>
            </SettingsCard>
          </TabsContent>
        </Tabs>
      </fieldset>

      {validationError && (
        <Alert variant="destructive">
          <Settings2 />
          <AlertTitle>配置无效</AlertTitle>
          <AlertDescription>{validationError}</AlertDescription>
        </Alert>
      )}

      <div className="sticky bottom-4 z-20 flex flex-wrap items-center gap-3 rounded-xl border bg-popover/95 p-3 shadow-lg backdrop-blur-md">
        <Button onClick={onSave} disabled={save.isPending || !dirty}>
          {save.isPending && <Spinner data-icon="inline-start" />}
          {save.isPending ? "保存中" : "保存设置"}
        </Button>
        <Button
          variant="ghost"
          onClick={onDiscard}
          disabled={save.isPending || !dirty}
        >
          <Undo2 data-icon="inline-start" />
          放弃修改
        </Button>
        <span
          aria-live="polite"
          className="ml-auto text-xs text-muted-foreground"
        >
          {dirty ? "有未保存的修改" : "所有修改已保存"}
        </span>
      </div>
    </div>
  );
}

function SettingField({
  id,
  label,
  description,
  children,
}: {
  id: string;
  label: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {children}
      {description && <FieldDescription>{description}</FieldDescription>}
    </Field>
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
      <CardContent>{children}</CardContent>
    </Card>
  );
}

function SettingsSkeleton() {
  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-5 w-3/4" />
      </div>
      <Skeleton className="h-8 w-full max-w-md" />
      {Array.from({ length: 2 }).map((_, index) => (
        <Card key={index}>
          <CardHeader>
            <Skeleton className="h-5 w-36" />
            <Skeleton className="h-4 w-2/3" />
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
