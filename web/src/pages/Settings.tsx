import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DEFAULT_DEV_UPDATE_MANIFEST_URL,
  DEFAULT_UPDATE_MANIFEST_URL,
} from "@ao3hub/shared";
import { Check, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { api } from "../lib/api";

type LocalConfig = {
  llm: {
    baseURL: string;
    apiKey: string;
    model: string;
    temperature: number;
    concurrency: number;
    blocksPerRequest: number;
  };
  ao3: {
    cookie: string;
    userAgent: string;
  };
  update: {
    manifestURL: string;
    channel: string;
    autoCheck: boolean;
  };
};

const defaultManifestURLForChannel = (channel: string) =>
  channel.trim().toLowerCase() === "dev"
    ? DEFAULT_DEV_UPDATE_MANIFEST_URL
    : DEFAULT_UPDATE_MANIFEST_URL;

const DEFAULT_MANIFEST_URLS = new Set([
  DEFAULT_UPDATE_MANIFEST_URL,
  DEFAULT_DEV_UPDATE_MANIFEST_URL,
]);

export function Settings() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["config"],
    queryFn: () => api.getConfig(),
  });

  const [form, setForm] = useState<LocalConfig | null>(null);
  const [apiKeyDirty, setApiKeyDirty] = useState(false);
  const [cookieDirty, setCookieDirty] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);

  useEffect(() => {
    if (!data) return;
    setForm({
      llm: {
        baseURL: data.llm.baseURL,
        apiKey: data.llm.apiKey,
        model: data.llm.model,
        temperature: data.llm.temperature,
        concurrency: data.llm.concurrency,
        blocksPerRequest: data.llm.blocksPerRequest,
      },
      ao3: { cookie: data.ao3.cookie, userAgent: data.ao3.userAgent },
      update: {
        manifestURL: data.update.manifestURL,
        channel: data.update.channel,
        autoCheck: data.update.autoCheck,
      },
    });
  }, [data]);

  const save = useMutation({
    mutationFn: (body: any) => api.saveConfig(body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["config"] }),
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
  });

  if (isLoading || !form)
    return <p className="text-muted-foreground">载入配置…</p>;

  const onSave = () => {
    const body: any = {
      llm: { ...form.llm },
      ao3: { ...form.ao3 },
      update: { ...form.update },
    };
    if (!apiKeyDirty) delete body.llm.apiKey;
    if (!cookieDirty) delete body.ao3.cookie;
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

  return (
    <div className="mx-auto max-w-[720px] space-y-12 fade-in">
      <header>
        <h1 className="text-[clamp(2rem,5vw,3rem)] font-semibold tracking-tight">
          Settings
        </h1>
        <p className="text-muted-foreground mt-3 text-[14px]">
          配置 LLM provider、AO3 cookie、OTA manifest。所有数据存在服务端 data/config.json。
        </p>
      </header>

      <section className="space-y-5">
        <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted-foreground">
          LLM Provider
        </h2>
        <Field id="llm-baseurl" label="Base URL">
          <Input
            id="llm-baseurl"
            value={form.llm.baseURL}
            onChange={(e) =>
              setForm({ ...form, llm: { ...form.llm, baseURL: e.target.value } })
            }
          />
        </Field>
        <Field
          id="llm-apikey"
          label={`API Key${data?.llm.hasApiKey ? "（已配置，留空保留）" : ""}`}
        >
          <Input
            id="llm-apikey"
            type="password"
            placeholder={data?.llm.hasApiKey ? "已存在 — 输入新值替换" : "sk-…"}
            value={apiKeyDirty ? form.llm.apiKey : ""}
            onChange={(e) => {
              setApiKeyDirty(true);
              setForm({ ...form, llm: { ...form.llm, apiKey: e.target.value } });
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
        <div className="grid grid-cols-3 gap-4">
          <Field id="llm-temp" label="Temperature">
            <Input
              id="llm-temp"
              type="number"
              step="0.1"
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
              min="1"
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
              min="1"
              value={form.llm.blocksPerRequest}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: { ...form.llm, blocksPerRequest: Number(e.target.value) },
                })
              }
            />
          </Field>
        </div>
        <div className="flex items-center gap-3">
          <Button
            variant="outline"
            onClick={() => test.mutate()}
            disabled={test.isPending}
          >
            {test.isPending ? "测试中…" : "测试连通"}
          </Button>
          {testResult && (
            <span
              className={`inline-flex items-center gap-1 text-[12px] ${
                testResult.ok ? "text-success" : "text-destructive"
              }`}
            >
              {testResult.ok ? (
                <Check className="size-3.5" />
              ) : (
                <X className="size-3.5" />
              )}
              {testResult.msg}
            </span>
          )}
        </div>
      </section>

      <section className="space-y-5">
        <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted-foreground">
          AO3
        </h2>
        <Field
          id="ao3-cookie"
          label={`Cookie${data?.ao3.hasCookie ? "（已配置，留空保留）" : ""}`}
        >
          <Textarea
            id="ao3-cookie"
            rows={3}
            placeholder={
              data?.ao3.hasCookie ? "已存在 — 输入新值替换" : "_otwarchive_session=…"
            }
            value={cookieDirty ? form.ao3.cookie : ""}
            onChange={(e) => {
              setCookieDirty(true);
              setForm({ ...form, ao3: { ...form.ao3, cookie: e.target.value } });
            }}
          />
        </Field>
        <Field id="ao3-ua" label="User Agent">
          <Input
            id="ao3-ua"
            value={form.ao3.userAgent}
            onChange={(e) =>
              setForm({ ...form, ao3: { ...form.ao3, userAgent: e.target.value } })
            }
          />
        </Field>
      </section>

      <section className="space-y-5">
        <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted-foreground">
          OTA Update
        </h2>
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
        <div className="grid grid-cols-2 gap-4">
          <Field id="ota-channel" label="Channel">
            <div id="ota-channel" className="flex gap-2">
              {["stable", "dev"].map((channel) => (
                <Button
                  key={channel}
                  type="button"
                  variant={form.update.channel === channel ? "default" : "outline"}
                  size="sm"
                  onClick={() => setUpdateChannel(channel)}
                >
                  {channel}
                </Button>
              ))}
            </div>
          </Field>
          <div className="flex items-end gap-3 pb-2">
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
            <label htmlFor="ota-auto" className="text-[13px] leading-none">
              启动时自动检查更新
            </label>
          </div>
        </div>
      </section>

      <Separator />

      <div className="flex items-center gap-3">
        <Button variant="default" onClick={onSave} disabled={save.isPending}>
          {save.isPending ? "保存中…" : "保存"}
        </Button>
        {save.isError && (
          <span className="text-destructive text-[12px]">
            {(save.error as Error).message}
          </span>
        )}
        {save.isSuccess && (
          <span className="inline-flex items-center gap-1 text-success text-[12px]">
            <Check className="size-3.5" />
            已保存
          </span>
        )}
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
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  );
}
