import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertCircle,
  Download,
  FileCode2,
  Gauge,
  Link2,
  Sparkles,
  UploadCloud,
} from "lucide-react";
import type { TranslationMode } from "@ao3hub/shared";
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
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/PageHeader";
import { cn } from "@/lib/utils";
import { api } from "../lib/api";

type Mode = "upload" | "url";
type ModeChoice = "default" | TranslationMode;

const MODE_COPY: Record<TranslationMode, { label: string; blurb: string }> = {
  normal: {
    label: "普通翻译",
    blurb: "直接分块翻译，速度更快、成本更低，适合快速阅读。",
  },
  refined: {
    label: "精翻模式",
    blurb:
      "先通读全文，生成摘要、角色术语与叙事基调，再分块翻译。质量更高，首次处理更慢、token 成本也更高。",
  },
};

const AO3_WORK_RE =
  /^https?:\/\/(?:www\.)?archiveofourown\.org\/works\/\d+(?:[/?#].*)?$/i;

function urlHint(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  if (AO3_WORK_RE.test(trimmed)) return null;
  if (/^https?:\/\//i.test(trimmed)) {
    return "看起来不是 AO3 作品页地址，正确格式为 archiveofourown.org/works/<id>。";
  }
  return "请填写完整链接，包含 https://。";
}

export function ImportPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [mode, setMode] = useState<Mode>("upload");
  const [url, setUrl] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [modeChoice, setModeChoice] = useState<ModeChoice>("default");
  const requestPendingRef = useRef(false);
  const dragDepthRef = useRef(0);

  const { data: config } = useQuery({
    queryKey: ["config", "public"],
    queryFn: ({ signal }) => api.getPublicConfig(signal),
  });
  const defaultMode = config?.llm.mode;
  const requestMode = modeChoice === "default" ? undefined : modeChoice;

  const onDone = (id: string) => {
    qc.invalidateQueries({ queryKey: ["stories"] });
    toast.success("已加入翻译队列", {
      description: "可以直接开始阅读，译文会随进度陆续填充。",
    });
    navigate({ to: "/r/$id/$chapter", params: { id, chapter: "0" } });
  };

  const upload = useMutation({
    mutationFn: (file: File) => api.uploadHtml(file, requestMode),
    onSuccess: (data) => onDone(data.id),
    onError: (uploadError: Error) => setError(uploadError.message),
    onSettled: () => {
      requestPendingRef.current = false;
    },
  });

  const create = useMutation({
    mutationFn: (workUrl: string) => api.createFromUrl(workUrl, requestMode),
    onSuccess: (data) => onDone(data.id),
    onError: (createError: Error) => setError(createError.message),
    onSettled: () => {
      requestPendingRef.current = false;
    },
  });
  const isPending = upload.isPending || create.isPending;

  const handleFiles = useCallback(
    (files: FileList | null) => {
      setError(null);
      if (!files?.[0] || requestPendingRef.current) return;
      const file = files[0];
      if (!/\.html?$/i.test(file.name) && !file.type.includes("html")) {
        setError("请选择 AO3 导出的 .html 文件");
        return;
      }
      requestPendingRef.current = true;
      setMode("upload");
      upload.mutate(file);
    },
    [upload],
  );

  // Dropping anywhere on the page works — hunting for the dashed rectangle is
  // busywork when the whole screen is "the import page".
  useEffect(() => {
    const onDragEnter = (event: DragEvent) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      dragDepthRef.current += 1;
      setDragOver(true);
    };
    const onDragOver = (event: DragEvent) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      event.preventDefault();
    };
    const onDragLeave = () => {
      dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
      if (dragDepthRef.current === 0) setDragOver(false);
    };
    const onDrop = (event: DragEvent) => {
      if (!event.dataTransfer?.types.includes("Files")) return;
      event.preventDefault();
      dragDepthRef.current = 0;
      setDragOver(false);
      handleFiles(event.dataTransfer.files);
    };

    window.addEventListener("dragenter", onDragEnter);
    window.addEventListener("dragover", onDragOver);
    window.addEventListener("dragleave", onDragLeave);
    window.addEventListener("drop", onDrop);
    return () => {
      window.removeEventListener("dragenter", onDragEnter);
      window.removeEventListener("dragover", onDragOver);
      window.removeEventListener("dragleave", onDragLeave);
      window.removeEventListener("drop", onDrop);
    };
  }, [handleFiles]);

  // Pasting an AO3 link anywhere on the page jumps straight to the URL form.
  useEffect(() => {
    const onPaste = (event: ClipboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.isContentEditable ||
          ["INPUT", "TEXTAREA"].includes(target.tagName))
      ) {
        return;
      }
      const text = event.clipboardData?.getData("text")?.trim();
      if (!text || !AO3_WORK_RE.test(text)) return;
      event.preventDefault();
      setMode("url");
      setUrl(text);
      setError(null);
      toast.info("已粘贴 AO3 链接");
    };
    window.addEventListener("paste", onPaste);
    return () => window.removeEventListener("paste", onPaste);
  }, []);

  const hint = urlHint(url);

  return (
    <div className="fade-in flex flex-col gap-6">
      <PageHeader
        title="添加作品"
        description="上传 AO3「Download → HTML」导出的文件，或粘贴作品链接由服务端抓取。导入后会直接进入阅读器，翻译在后台继续。"
      />

      {error && (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>导入失败</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle>作品来源</CardTitle>
            <CardDescription>
              也可以把文件拖到页面任意位置，或直接粘贴 AO3 链接。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Tabs
              value={mode}
              onValueChange={(value) => {
                setMode(value as Mode);
                setError(null);
              }}
            >
              <TabsList className="w-full">
                <TabsTrigger value="upload" disabled={isPending}>
                  <UploadCloud data-icon="inline-start" />
                  上传 HTML
                </TabsTrigger>
                <TabsTrigger value="url" disabled={isPending}>
                  <Link2 data-icon="inline-start" />
                  AO3 链接
                </TabsTrigger>
              </TabsList>

              <TabsContent value="upload">
                <label
                  className={cn(
                    "flex min-h-60 flex-col items-center justify-center gap-4 rounded-xl border border-dashed p-8 text-center transition-colors",
                    dragOver ? "border-primary bg-primary/5" : "border-input",
                    isPending
                      ? "pointer-events-none opacity-60"
                      : "cursor-pointer hover:border-primary/50 hover:bg-muted/50",
                  )}
                >
                  <Input
                    type="file"
                    accept=".html,text/html"
                    className="sr-only"
                    onChange={(event) => {
                      handleFiles(event.currentTarget.files);
                      event.currentTarget.value = "";
                    }}
                    disabled={isPending}
                  />
                  <div className="flex size-10 items-center justify-center rounded-lg bg-muted text-foreground">
                    {upload.isPending ? (
                      <Spinner />
                    ) : (
                      <UploadCloud className="size-5" />
                    )}
                  </div>
                  <div className="flex flex-col gap-1">
                    <p className="text-sm font-medium">
                      {upload.isPending
                        ? "正在解析作品…"
                        : dragOver
                          ? "松手即可导入"
                          : "拖入 HTML 文件，或点击选择"}
                    </p>
                    <p className="text-sm text-muted-foreground">
                      接受 AO3 原始导出的 .html 文件
                    </p>
                  </div>
                </label>
              </TabsContent>

              <TabsContent value="url">
                <form
                  onSubmit={(event) => {
                    event.preventDefault();
                    setError(null);
                    if (!url.trim() || requestPendingRef.current) return;
                    requestPendingRef.current = true;
                    create.mutate(url.trim());
                  }}
                >
                  <FieldGroup>
                    <Field
                      data-disabled={isPending || undefined}
                      data-invalid={hint ? true : undefined}
                    >
                      <FieldLabel htmlFor="ao3-url">AO3 作品链接</FieldLabel>
                      <Input
                        id="ao3-url"
                        type="url"
                        inputMode="url"
                        placeholder="https://archiveofourown.org/works/12345678"
                        value={url}
                        onChange={(event) => setUrl(event.target.value)}
                        aria-invalid={hint ? true : undefined}
                        required
                        autoFocus
                        disabled={isPending}
                      />
                      {hint ? (
                        <FieldError>{hint}</FieldError>
                      ) : (
                        <FieldDescription>
                          受限或 Explicit 作品可能需要先在设置中填写 AO3
                          Cookie。
                        </FieldDescription>
                      )}
                    </Field>
                    <Field orientation="horizontal">
                      <Button
                        type="submit"
                        disabled={isPending || !url.trim() || !!hint}
                      >
                        {create.isPending ? (
                          <Spinner data-icon="inline-start" />
                        ) : (
                          <Download data-icon="inline-start" />
                        )}
                        {create.isPending ? "抓取中" : "下载并翻译"}
                      </Button>
                      {url && (
                        <Button
                          type="button"
                          variant="ghost"
                          disabled={isPending}
                          onClick={() => setUrl("")}
                        >
                          清空
                        </Button>
                      )}
                    </Field>
                  </FieldGroup>
                </form>
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>翻译策略</CardTitle>
            <CardDescription>为这次导入选择速度与质量的平衡。</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet data-disabled={isPending || undefined}>
              <FieldLegend className="sr-only">翻译模式</FieldLegend>
              <RadioGroup
                value={modeChoice}
                onValueChange={(value) => setModeChoice(value as ModeChoice)}
                disabled={isPending}
              >
                <ModeOption
                  value="default"
                  icon={Gauge}
                  title="跟随默认"
                  badge={defaultMode ? MODE_COPY[defaultMode].label : undefined}
                  description="使用设置页里配置的默认模式。"
                />
                <ModeOption
                  value="normal"
                  icon={FileCode2}
                  title={MODE_COPY.normal.label}
                  description={MODE_COPY.normal.blurb}
                />
                <ModeOption
                  value="refined"
                  icon={Sparkles}
                  title={MODE_COPY.refined.label}
                  description={MODE_COPY.refined.blurb}
                />
              </RadioGroup>
              <FieldDescription>
                长篇、设定密集的作品优先选精翻；短篇或只想快速预览时用普通模式。导入后仍可在翻译状态面板里重新预读。
              </FieldDescription>
            </FieldSet>
          </CardContent>
        </Card>
      </div>

      {dragOver && (
        <div
          className="pointer-events-none fixed inset-0 z-50 flex items-center justify-center bg-background/70 backdrop-blur-sm"
          aria-hidden
        >
          <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-primary bg-card px-10 py-8 text-center shadow-lg">
            <UploadCloud className="size-8 text-primary" />
            <p className="text-sm font-medium">松手即可导入 HTML</p>
          </div>
        </div>
      )}
    </div>
  );
}

/** A selectable card: the shadcn "choice card" recipe built from Field + Radio. */
function ModeOption({
  value,
  icon: Icon,
  title,
  badge,
  description,
}: {
  value: ModeChoice;
  icon: typeof Gauge;
  title: string;
  badge?: string;
  description: string;
}) {
  const id = `mode-${value}`;
  return (
    <FieldLabel htmlFor={id}>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldTitle>
            <Icon className="size-4 text-muted-foreground" />
            {title}
            {badge && <Badge variant="secondary">{badge}</Badge>}
          </FieldTitle>
          <FieldDescription>{description}</FieldDescription>
        </FieldContent>
        <RadioGroupItem value={value} id={id} />
      </Field>
    </FieldLabel>
  );
}
