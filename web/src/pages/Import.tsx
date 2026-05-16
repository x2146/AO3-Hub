import { useCallback, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Download, UploadCloud } from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
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
        <p className="text-muted-foreground mt-3 text-[14px] leading-relaxed">
          直接把 AO3「Download → HTML」生成的文件拖进来；或者贴 work URL，服务端代为下载。
        </p>
      </header>

      <Tabs
        value={mode}
        onValueChange={(v) => {
          setMode(v as Mode);
          setError(null);
        }}
      >
        <TabsList>
          <TabsTrigger value="upload" className="gap-1.5">
            <UploadCloud className="size-3.5" />
            上传 HTML
          </TabsTrigger>
          <TabsTrigger value="url" className="gap-1.5">
            <Download className="size-3.5" />
            贴 URL
          </TabsTrigger>
        </TabsList>

        <TabsContent value="upload">
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
            className={cn(
              "flex flex-col items-center justify-center gap-2 rounded-3xl border-2 border-dashed border-border p-12 text-center transition-colors",
              dragOver && "border-accent bg-accent/5",
              upload.isPending
                ? "opacity-60"
                : "cursor-pointer hover:bg-secondary/50",
            )}
          >
            <input
              type="file"
              accept=".html,text/html"
              className="hidden"
              onChange={(e) => handleFiles(e.target.files)}
              disabled={upload.isPending}
            />
            <UploadCloud className="size-7 text-muted-foreground" />
            <p className="text-[20px] font-semibold">
              {upload.isPending ? "处理中…" : "拖文件到这里"}
            </p>
            <p className="text-muted-foreground text-[13px]">
              或点击选取 — 支持 AO3 「Download → HTML」原始导出
            </p>
          </label>
        </TabsContent>

        <TabsContent value="url">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setError(null);
              if (!url.trim()) return;
              create.mutate(url.trim());
            }}
            className="space-y-4"
          >
            <div className="space-y-2">
              <Label htmlFor="ao3-url">AO3 work URL</Label>
              <Input
                id="ao3-url"
                type="url"
                placeholder="https://archiveofourown.org/works/12345678"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                required
                autoFocus
                disabled={create.isPending}
              />
            </div>
            <Button
              type="submit"
              variant="default"
              disabled={create.isPending || !url.trim()}
            >
              {create.isPending ? "抓取中…" : "下载并翻译"}
            </Button>
            <p className="text-muted-foreground text-[12px] leading-relaxed">
              如果作品标记为 Explicit，需要在 Settings 里填 AO3 cookie。
            </p>
          </form>
        </TabsContent>
      </Tabs>

      {error && (
        <div className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-[13px] text-destructive">
          <AlertCircle className="mt-0.5 size-3.5 shrink-0" />
          <span>{error}</span>
        </div>
      )}
    </div>
  );
}
