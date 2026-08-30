import { useCallback, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Download, FileCode2, Sparkles, UploadCloud } from "lucide-react";
import type { TranslationMode } from "@ao3hub/shared";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
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
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import { api } from "../lib/api";

type Mode = "upload" | "url";
type ModeChoice = "default" | TranslationMode;

export function ImportPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [mode, setMode] = useState<Mode>("upload");
  const [url, setUrl] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [modeChoice, setModeChoice] = useState<ModeChoice>("default");
  const requestPendingRef = useRef(false);

  const { data: config } = useQuery({
    queryKey: ["config", "public"],
    queryFn: ({ signal }) => api.getPublicConfig(signal),
  });
  const defaultMode = config?.llm.mode;
  const effectiveMode: TranslationMode | undefined =
    modeChoice === "default" ? defaultMode : modeChoice;
  const requestMode = modeChoice === "default" ? undefined : modeChoice;

  const onDone = (id: string) => {
    qc.invalidateQueries({ queryKey: ["stories"] });
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
      upload.mutate(file);
    },
    [upload],
  );

  return (
    <div className="flex flex-col gap-8 fade-in">
      <div className="flex flex-col gap-2">
        <Badge variant="accent">
          <FileCode2 />
          Import workflow
        </Badge>
        <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">添加新作品</h1>
        <p className="max-w-2xl text-sm leading-relaxed text-muted-foreground">
          上传 AO3「Download → HTML」导出的文件，或粘贴作品链接由服务端自动抓取。
        </p>
      </div>

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
        <Card>
          <CardHeader>
            <CardTitle>翻译策略</CardTitle>
            <CardDescription>为这次导入选择速度与质量的平衡。</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet data-disabled={isPending || undefined}>
              <FieldLegend className="sr-only">翻译模式</FieldLegend>
              <ToggleGroup
                type="single"
                value={modeChoice}
                onValueChange={(value) => value && setModeChoice(value as ModeChoice)}
                variant="outline"
                spacing={2}
                className="flex w-full flex-col items-stretch sm:flex-row lg:flex-col"
                disabled={isPending}
              >
                <ToggleGroupItem value="default" className="justify-start">
                  跟随默认
                  {defaultMode && (
                    <Badge variant="secondary">
                      {defaultMode === "refined" ? "精翻" : "普通"}
                    </Badge>
                  )}
                </ToggleGroupItem>
                <ToggleGroupItem value="normal" className="justify-start">普通翻译</ToggleGroupItem>
                <ToggleGroupItem value="refined" className="justify-start">精翻模式</ToggleGroupItem>
              </ToggleGroup>
              <FieldDescription>
                {effectiveMode === undefined
                  ? "默认模式暂未读取，将使用服务端配置。"
                  : effectiveMode === "refined"
                    ? "先通读全文，生成摘要、角色术语与叙事基调，再分块翻译。质量更高，首次处理时间和 token 成本也更高。"
                    : "直接分块翻译，速度更快、成本更低，适合快速阅读。"}
              </FieldDescription>
            </FieldSet>
          </CardContent>
          <CardFooter>
            <Alert>
              <Sparkles />
              <AlertTitle>建议</AlertTitle>
              <AlertDescription>
                长篇、设定密集作品优先选择精翻；短篇或快速预览可使用普通模式。
              </AlertDescription>
            </Alert>
          </CardFooter>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>作品来源</CardTitle>
            <CardDescription>两种方式都会在后台创建翻译任务。</CardDescription>
          </CardHeader>
          <CardContent>
            <Tabs
              value={mode}
              onValueChange={(value) => {
                setMode(value as Mode);
                setError(null);
              }}
            >
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="upload" disabled={isPending}>
                  <UploadCloud />
                  上传 HTML
                </TabsTrigger>
                <TabsTrigger value="url" disabled={isPending}>
                  <Download />
                  AO3 链接
                </TabsTrigger>
              </TabsList>

              <TabsContent value="upload">
                <label
                  onDragOver={(event) => {
                    event.preventDefault();
                    setDragOver(true);
                  }}
                  onDragLeave={() => setDragOver(false)}
                  onDrop={(event) => {
                    event.preventDefault();
                    setDragOver(false);
                    handleFiles(event.dataTransfer.files);
                  }}
                  className={cn(
                    "flex min-h-72 flex-col items-center justify-center gap-3 rounded-xl border-2 border-dashed p-8 text-center transition-[border-color,background-color,transform]",
                    dragOver && "scale-[1.01] border-primary bg-primary/5",
                    isPending
                      ? "pointer-events-none opacity-60"
                      : "cursor-pointer hover:border-primary/45 hover:bg-muted/50",
                  )}
                >
                  <Input
                    type="file"
                    accept=".html,text/html"
                    className="hidden"
                    onChange={(event) => {
                      handleFiles(event.currentTarget.files);
                      event.currentTarget.value = "";
                    }}
                    disabled={isPending}
                  />
                  <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10 text-primary">
                    {upload.isPending ? <Spinner /> : <UploadCloud />}
                  </div>
                  <div className="flex flex-col gap-1">
                    <p className="font-semibold">
                      {upload.isPending ? "正在解析作品" : "拖放 HTML 到这里"}
                    </p>
                    <p className="text-sm text-muted-foreground">
                      或点击选择 AO3 原始 HTML 导出文件
                    </p>
                  </div>
                  <Badge variant="secondary">.html</Badge>
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
                    <Field data-disabled={isPending || undefined}>
                      <FieldLabel htmlFor="ao3-url">AO3 work URL</FieldLabel>
                      <Input
                        id="ao3-url"
                        type="url"
                        placeholder="https://archiveofourown.org/works/12345678"
                        value={url}
                        onChange={(event) => setUrl(event.target.value)}
                        required
                        autoFocus
                        disabled={isPending}
                      />
                      <FieldDescription>
                        Explicit 作品可能需要先在设置中填写 AO3 Cookie。
                      </FieldDescription>
                    </Field>
                    <Field orientation="horizontal">
                      <Button type="submit" disabled={isPending || !url.trim()}>
                        {create.isPending ? (
                          <Spinner data-icon="inline-start" />
                        ) : (
                          <Download data-icon="inline-start" />
                        )}
                        {create.isPending ? "抓取中" : "下载并翻译"}
                      </Button>
                    </Field>
                  </FieldGroup>
                </form>
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>导入失败</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
    </div>
  );
}
