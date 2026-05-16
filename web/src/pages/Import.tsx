import { useCallback, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

type Mode = "upload" | "url";

export function ImportPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [mode, setMode] = useState<Mode>("upload");
  const [url, setUrl] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onDone = (id: string) => {
    qc.invalidateQueries({ queryKey: ["stories"] });
    navigate({ to: "/r/$id/$chapter", params: { id, chapter: "0" } });
  };

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadHtml(file),
    onSuccess: (d) => onDone(d.id),
    onError: (e: Error) => setError(e.message),
  });

  const create = useMutation({
    mutationFn: (u: string) => api.createFromUrl(u),
    onSuccess: (d) => onDone(d.id),
    onError: (e: Error) => setError(e.message),
  });

  const handleFiles = useCallback(
    (files: FileList | null) => {
      setError(null);
      if (!files || !files[0]) return;
      const f = files[0];
      if (!/\.html?$/i.test(f.name) && !f.type.includes("html")) {
        setError("文件应为 .html");
        return;
      }
      upload.mutate(f);
    },
    [upload],
  );

  return (
    <div className="mx-auto max-w-[680px] space-y-10 fade-in">
      <header>
        <h1 className="text-[clamp(2rem,5vw,3rem)] font-semibold leading-[1.05] tracking-tight">
          添加作品
        </h1>
        <p className="text-muted mt-3 text-[14px] leading-relaxed">
          直接把 AO3「Download → HTML」生成的文件拖进来；或者贴 work URL，服务端代为下载。
        </p>
      </header>

      <div className="flex gap-1 rounded-full border rule p-1 text-[13px] font-medium w-fit">
        {(
          [
            { id: "upload" as Mode, label: "上传 HTML" },
            { id: "url" as Mode, label: "贴 URL" },
          ]
        ).map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => {
              setMode(t.id);
              setError(null);
            }}
            className={`rounded-full px-4 py-1.5 transition-colors ${
              mode === t.id
                ? "bg-[rgb(var(--ink))] text-[rgb(var(--paper))]"
                : "text-muted hover:text-[rgb(var(--ink))]"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {mode === "upload" && (
        <label
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => {
            e.preventDefault();
            setDragOver(false);
            handleFiles(e.dataTransfer.files);
          }}
          className={`flex flex-col items-center justify-center gap-2 rounded-3xl border-2 border-dashed rule p-12 text-center transition-colors ${
            dragOver ? "border-accent bg-[rgb(var(--accent)/0.05)]" : ""
          } ${upload.isPending ? "opacity-60" : "cursor-pointer hover:bg-[rgb(var(--ink)/0.03)]"}`}
        >
          <input
            type="file"
            accept=".html,text/html"
            className="hidden"
            onChange={(e) => handleFiles(e.target.files)}
            disabled={upload.isPending}
          />
          <p className="text-[20px] font-semibold">
            {upload.isPending ? "处理中…" : "拖文件到这里"}
          </p>
          <p className="text-muted text-[13px]">
            或点击选取 — 支持 AO3 「Download → HTML」原始导出
          </p>
        </label>
      )}

      {mode === "url" && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            if (!url.trim()) return;
            create.mutate(url.trim());
          }}
          className="space-y-4"
        >
          <label className="block">
            <span className="text-muted text-[12px] tracking-wider uppercase">
              AO3 work URL
            </span>
            <input
              type="url"
              className="input mt-2"
              placeholder="https://archiveofourown.org/works/12345678"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              required
              autoFocus
              disabled={create.isPending}
            />
          </label>
          <button
            type="submit"
            className="btn btn-primary"
            disabled={create.isPending || !url.trim()}
          >
            {create.isPending ? "抓取中…" : "下载并翻译"}
          </button>
          <p className="text-muted text-[12px] leading-relaxed">
            如果作品标记为 Explicit，需要在 Settings 里填 AO3 cookie。
          </p>
        </form>
      )}

      {error && (
        <p className="rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-[13px] text-red-500">
          {error}
        </p>
      )}
    </div>
  );
}
