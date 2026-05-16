import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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

export function Settings() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["config"],
    queryFn: () => api.getConfig(),
  });

  const [form, setForm] = useState<LocalConfig | null>(null);
  const [apiKeyDirty, setApiKeyDirty] = useState(false);
  const [cookieDirty, setCookieDirty] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);

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
        r.ok ? `✓ 通过：${(r.content ?? "").slice(0, 80)}` : `× ${r.error ?? "失败"}`,
      );
    },
  });

  if (isLoading || !form) return <p className="text-muted">载入配置…</p>;

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

  return (
    <div className="mx-auto max-w-[720px] space-y-12 fade-in">
      <header>
        <h1 className="text-[clamp(2rem,5vw,3rem)] font-semibold tracking-tight">
          Settings
        </h1>
        <p className="text-muted mt-3 text-[14px]">
          配置 LLM provider、AO3 cookie、OTA manifest。所有数据存在服务端 data/config.json。
        </p>
      </header>

      <section className="space-y-5">
        <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted">
          LLM Provider
        </h2>
        <Field label="Base URL">
          <input
            className="input"
            value={form.llm.baseURL}
            onChange={(e) =>
              setForm({ ...form, llm: { ...form.llm, baseURL: e.target.value } })
            }
          />
        </Field>
        <Field label={`API Key${data?.llm.hasApiKey ? "（已配置，留空保留）" : ""}`}>
          <input
            type="password"
            className="input"
            placeholder={data?.llm.hasApiKey ? "已存在 — 输入新值替换" : "sk-…"}
            value={apiKeyDirty ? form.llm.apiKey : ""}
            onChange={(e) => {
              setApiKeyDirty(true);
              setForm({ ...form, llm: { ...form.llm, apiKey: e.target.value } });
            }}
          />
        </Field>
        <Field label="Model">
          <input
            className="input"
            value={form.llm.model}
            onChange={(e) =>
              setForm({ ...form, llm: { ...form.llm, model: e.target.value } })
            }
          />
        </Field>
        <div className="grid grid-cols-3 gap-4">
          <Field label="Temperature">
            <input
              type="number"
              step="0.1"
              className="input"
              value={form.llm.temperature}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: { ...form.llm, temperature: Number(e.target.value) },
                })
              }
            />
          </Field>
          <Field label="Concurrency">
            <input
              type="number"
              min="1"
              className="input"
              value={form.llm.concurrency}
              onChange={(e) =>
                setForm({
                  ...form,
                  llm: { ...form.llm, concurrency: Number(e.target.value) },
                })
              }
            />
          </Field>
          <Field label="Blocks / request">
            <input
              type="number"
              min="1"
              className="input"
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
          <button
            type="button"
            className="btn btn-ghost"
            onClick={() => test.mutate()}
            disabled={test.isPending}
          >
            {test.isPending ? "测试中…" : "测试连通"}
          </button>
          {testResult && (
            <span
              className={`text-[12px] ${
                testResult.startsWith("✓") ? "text-emerald-500" : "text-red-500"
              }`}
            >
              {testResult}
            </span>
          )}
        </div>
      </section>

      <section className="space-y-5">
        <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted">
          AO3
        </h2>
        <Field label={`Cookie${data?.ao3.hasCookie ? "（已配置，留空保留）" : ""}`}>
          <textarea
            rows={3}
            className="input"
            placeholder={data?.ao3.hasCookie ? "已存在 — 输入新值替换" : "_otwarchive_session=…"}
            value={cookieDirty ? form.ao3.cookie : ""}
            onChange={(e) => {
              setCookieDirty(true);
              setForm({ ...form, ao3: { ...form.ao3, cookie: e.target.value } });
            }}
          />
        </Field>
        <Field label="User Agent">
          <input
            className="input"
            value={form.ao3.userAgent}
            onChange={(e) =>
              setForm({ ...form, ao3: { ...form.ao3, userAgent: e.target.value } })
            }
          />
        </Field>
      </section>

      <section className="space-y-5">
        <h2 className="text-[14px] font-semibold tracking-wider uppercase text-muted">
          OTA Update
        </h2>
        <Field label="Manifest URL">
          <input
            className="input"
            placeholder="https://example.com/ao3-hub/manifest.json"
            value={form.update.manifestURL}
            onChange={(e) =>
              setForm({ ...form, update: { ...form.update, manifestURL: e.target.value } })
            }
          />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label="Channel">
            <input
              className="input"
              value={form.update.channel}
              onChange={(e) =>
                setForm({ ...form, update: { ...form.update, channel: e.target.value } })
              }
            />
          </Field>
          <label className="flex items-end gap-2 text-[13px]">
            <input
              type="checkbox"
              checked={form.update.autoCheck}
              onChange={(e) =>
                setForm({
                  ...form,
                  update: { ...form.update, autoCheck: e.target.checked },
                })
              }
            />
            <span>启动时自动检查更新</span>
          </label>
        </div>
      </section>

      <div className="flex items-center gap-3 border-t rule pt-6">
        <button
          type="button"
          className="btn btn-primary"
          onClick={onSave}
          disabled={save.isPending}
        >
          {save.isPending ? "保存中…" : "保存"}
        </button>
        {save.isError && (
          <span className="text-red-500 text-[12px]">
            {(save.error as Error).message}
          </span>
        )}
        {save.isSuccess && <span className="text-emerald-500 text-[12px]">已保存</span>}
      </div>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="text-muted text-[11px] tracking-wider uppercase">{label}</span>
      <div className="mt-1.5">{children}</div>
    </label>
  );
}
