import { useEffect, useMemo, useRef, useSyncExternalStore } from "react";
import { Link } from "@tanstack/react-router";
import { Check, Keyboard, RotateCcw } from "lucide-react";
import { RadioGroup as RadioGroupPrimitive } from "radix-ui";
import type { ChapterView } from "@ao3hub/shared";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import {
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Separator } from "@/components/ui/separator";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import {
  DEFAULT_READER_SETTINGS,
  loadReadingPositions,
  READER_LIMITS,
  READER_PAPERS,
  type ReaderDefaults,
  type ReaderPaper,
  type ReaderSettings,
  type ReaderTypeface,
  type ReaderView,
} from "@/lib/reader-settings";
import { VIEW_OPTIONS } from "./ReaderChrome";
import { chapterHeading } from "./chapter-title";

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ["←"], label: "上一章" },
  { keys: ["→"], label: "下一章" },
  { keys: ["T"], label: "章节目录" },
  { keys: ["S"], label: "阅读设置" },
  { keys: ["E"], label: "切换双语 / 仅中文" },
  { keys: ["Esc"], label: "关闭当前面板" },
];

const TYPEFACES: { value: ReaderTypeface; label: string; sample: string }[] = [
  { value: "serif", label: "衬线", sample: "font-serif" },
  { value: "sans", label: "无衬线", sample: "font-sans" },
];

function subscribeWide(onChange: () => void) {
  const media = window.matchMedia("(min-width: 640px)");
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

/** Sheets rise from the bottom on phones and slide in from the side above sm. */
export function useSheetSide(edge: "left" | "right"): "bottom" | typeof edge {
  const wide = useSyncExternalStore(
    subscribeWide,
    () => window.matchMedia("(min-width: 640px)").matches,
  );
  return wide ? edge : "bottom";
}

const BOTTOM_SHEET =
  "data-[side=bottom]:max-h-[85svh] data-[side=bottom]:rounded-t-2xl data-[side=bottom]:pb-[env(safe-area-inset-bottom)]";

export function SettingsSheet({
  settings,
  setSettings,
  defaults,
  onCloseAutoFocus,
}: {
  settings: ReaderSettings;
  setSettings: (updater: (current: ReaderSettings) => ReaderSettings) => void;
  defaults: ReaderDefaults;
  onCloseAutoFocus: (event: Event) => void;
}) {
  const side = useSheetSide("right");
  const patch = (next: Partial<ReaderSettings>) =>
    setSettings((current) => ({ ...current, ...next }));

  return (
    <SheetContent
      side={side}
      onCloseAutoFocus={onCloseAutoFocus}
      className={cn(
        "gap-0 overflow-y-auto data-[side=right]:w-full sm:max-w-sm",
        BOTTOM_SHEET,
      )}
    >
      <SheetGrabber />
      <SheetHeader>
        <SheetTitle>阅读设置</SheetTitle>
        <SheetDescription>排版偏好保存在这台设备上。</SheetDescription>
      </SheetHeader>

      <div className="flex flex-col gap-6 px-4 pb-6">
        <FieldSet>
          <FieldLegend variant="label">页面</FieldLegend>
          <PaperPicker
            value={settings.paper}
            onChange={(paper) => patch({ paper })}
          />
        </FieldSet>

        <FieldSet>
          <FieldLegend variant="label">显示语言</FieldLegend>
          <Segmented
            value={settings.view}
            options={VIEW_OPTIONS.map((o) => ({
              value: o.value,
              label: o.label,
            }))}
            onChange={(view) => patch({ view: view as ReaderView })}
            label="显示语言"
          />
          <FieldDescription>
            {VIEW_OPTIONS.find((o) => o.value === settings.view)?.hint}
          </FieldDescription>
        </FieldSet>

        <Separator />

        <FieldSet>
          <FieldLegend variant="label">字体</FieldLegend>
          <Segmented
            value={settings.typeface}
            options={TYPEFACES.map((t) => ({
              value: t.value,
              label: (
                <span className={cn("text-[0.95rem]", t.sample)}>
                  {t.label} Aa
                </span>
              ),
            }))}
            onChange={(typeface) =>
              patch({ typeface: typeface as ReaderTypeface })
            }
            label="字体"
          />
          <div className="flex flex-col gap-5 pt-1">
            <ReaderSlider
              label="字号"
              value={settings.font}
              {...READER_LIMITS.font}
              format={(v) => `${v}px`}
              onChange={(v) => patch({ font: v })}
            />
            <ReaderSlider
              label="行距"
              value={settings.leading}
              {...READER_LIMITS.leading}
              format={(v) => v.toFixed(2)}
              onChange={(v) => patch({ leading: Number(v.toFixed(2)) })}
            />
            <ReaderSlider
              label="中文比例"
              value={settings.zh}
              {...READER_LIMITS.zh}
              format={(v) => `${Math.round(v * 100)}%`}
              onChange={(v) => patch({ zh: Number(v.toFixed(2)) })}
            />
            <ReaderSlider
              label="栏宽"
              value={settings.measure}
              {...READER_LIMITS.measure}
              format={(v) => `${v}px`}
              onChange={(v) => patch({ measure: v })}
            />
          </div>
        </FieldSet>

        <Separator />

        <div className="flex flex-col gap-4">
          <SwitchRow
            id="reader-notes"
            title="显示作者的话"
            description="简介之外的前言、章前注与章末注。"
            checked={settings.notes}
            onChange={(notes) => patch({ notes })}
          />
          <SwitchRow
            id="reader-focus"
            title="专注模式"
            description="淡出其他段落，只保留鼠标所在的一段。"
            checked={settings.focus}
            onChange={(focus) => patch({ focus })}
          />
        </div>

        <Button
          variant="outline"
          size="sm"
          className="self-start"
          onClick={() =>
            setSettings((current) => ({
              ...current,
              ...defaults,
              leading: DEFAULT_READER_SETTINGS.leading,
              typeface: DEFAULT_READER_SETTINGS.typeface,
            }))
          }
        >
          <RotateCcw data-icon="inline-start" />
          恢复默认排版
        </Button>

        <section className="hidden flex-col gap-2 sm:flex">
          <Separator className="mb-4" />
          <h3 className="flex items-center gap-2 text-sm font-medium">
            <Keyboard className="size-4" />
            键盘快捷键
          </h3>
          <dl className="flex flex-col gap-1.5 text-sm">
            {SHORTCUTS.map((shortcut) => (
              <div
                key={shortcut.label}
                className="flex items-center justify-between gap-4"
              >
                <dt className="text-muted-foreground">{shortcut.label}</dt>
                <dd>
                  <KbdGroup>
                    {shortcut.keys.map((key) => (
                      <Kbd key={key}>{key}</Kbd>
                    ))}
                  </KbdGroup>
                </dd>
              </div>
            ))}
          </dl>
        </section>
      </div>
    </SheetContent>
  );
}

function SheetGrabber() {
  return (
    <div
      aria-hidden
      className="mx-auto mt-2 h-1 w-10 shrink-0 rounded-full bg-muted-foreground/25 sm:hidden"
    />
  );
}

function PaperPicker({
  value,
  onChange,
}: {
  value: ReaderPaper;
  onChange: (paper: ReaderPaper) => void;
}) {
  return (
    <RadioGroupPrimitive.Root
      value={value}
      onValueChange={(next) => onChange(next as ReaderPaper)}
      aria-label="页面颜色"
      className="grid grid-cols-4 gap-2"
    >
      {READER_PAPERS.map((paper) => (
        <RadioGroupPrimitive.Item
          key={paper.value}
          value={paper.value}
          className="group/paper flex flex-col items-center gap-1.5 py-1 text-xs text-muted-foreground outline-none data-[state=checked]:text-foreground"
        >
          <span
            className={cn(
              "relative flex size-11 items-center justify-center rounded-full border text-sm font-medium shadow-xs transition-shadow",
              "group-focus-visible/paper:ring-3 group-focus-visible/paper:ring-ring/50",
              "group-data-[state=checked]/paper:ring-2 group-data-[state=checked]/paper:ring-primary group-data-[state=checked]/paper:ring-offset-2 group-data-[state=checked]/paper:ring-offset-background",
              !paper.background &&
                "bg-[linear-gradient(135deg,#fbfaf7_0_50%,#16171a_50%_100%)]",
            )}
            style={
              paper.background
                ? { background: paper.background, color: paper.ink }
                : undefined
            }
          >
            {paper.background && "文"}
            <RadioGroupPrimitive.Indicator className="absolute -right-0.5 -bottom-0.5 flex size-4 items-center justify-center rounded-full bg-primary text-primary-foreground">
              <Check className="size-2.5" strokeWidth={3} />
            </RadioGroupPrimitive.Indicator>
          </span>
          {paper.label}
        </RadioGroupPrimitive.Item>
      ))}
    </RadioGroupPrimitive.Root>
  );
}

function Segmented({
  value,
  options,
  onChange,
  label,
}: {
  value: string;
  options: { value: string; label: React.ReactNode }[];
  onChange: (value: string) => void;
  label: string;
}) {
  return (
    <ToggleGroup
      type="single"
      value={value}
      onValueChange={(next) => next && onChange(next)}
      spacing={0}
      className="w-full rounded-lg bg-muted p-0.5"
      aria-label={label}
    >
      {options.map((option) => (
        <ToggleGroupItem
          key={option.value}
          value={option.value}
          className="flex-1 rounded-md! data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm"
        >
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

function SwitchRow({
  id,
  title,
  description,
  checked,
  onChange,
}: {
  id: string;
  title: string;
  description: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <FieldLabel htmlFor={id}>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldTitle>{title}</FieldTitle>
          <FieldDescription>{description}</FieldDescription>
        </FieldContent>
        <Switch id={id} checked={checked} onCheckedChange={onChange} />
      </Field>
    </FieldLabel>
  );
}

function ReaderSlider({
  label,
  value,
  min,
  max,
  step,
  format,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  format: (v: number) => string;
  onChange: (v: number) => void;
}) {
  return (
    <Field>
      <div className="flex items-center justify-between gap-4">
        <FieldLabel>{label}</FieldLabel>
        <span className="text-xs tabular-nums text-muted-foreground">
          {format(value)}
        </span>
      </div>
      <Slider
        aria-label={label}
        value={[value]}
        min={min}
        max={max}
        step={step}
        onValueChange={(arr) => arr[0] !== undefined && onChange(arr[0])}
      />
    </Field>
  );
}

export function TocSheet({
  data,
  chapterIndex,
  onClose,
  onCloseAutoFocus,
}: {
  data: ChapterView;
  chapterIndex: number;
  onClose: () => void;
  onCloseAutoFocus: (event: Event) => void;
}) {
  const side = useSheetSide("left");
  const activeRef = useRef<HTMLAnchorElement>(null);
  // Read once per opening; the sheet unmounts when closed.
  const positions = useMemo(
    () => loadReadingPositions(data.meta.id),
    [data.meta.id],
  );

  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "center" });
  }, []);

  const digits = String(data.nav.total).length;
  return (
    <SheetContent
      side={side}
      onCloseAutoFocus={onCloseAutoFocus}
      className={cn(
        "gap-0 overflow-y-auto data-[side=left]:w-full sm:max-w-sm",
        BOTTOM_SHEET,
      )}
    >
      <SheetGrabber />
      <SheetHeader>
        <SheetTitle className="font-serif text-lg leading-snug">
          {data.meta.title}
        </SheetTitle>
        <SheetDescription>
          {data.meta.author} · 共 {data.nav.total} 章
        </SheetDescription>
      </SheetHeader>
      <ol className="flex flex-col gap-0.5 px-2 pb-6">
        {Array.from({ length: data.nav.total }, (_, i) => {
          const active = i === chapterIndex;
          const ratio = positions.get(i) ?? 0;
          const heading = chapterHeading(data.nav.titles[i], i);
          return (
            <li key={i}>
              <Link
                ref={active ? activeRef : undefined}
                to="/r/$id/$chapter"
                params={{ id: data.meta.id, chapter: String(i) }}
                onClick={onClose}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm outline-none transition-colors hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50",
                  active && "bg-accent text-accent-foreground",
                )}
              >
                <span className="shrink-0 font-mono text-xs text-muted-foreground tabular-nums">
                  {String(i + 1).padStart(Math.max(2, digits), "0")}
                </span>
                <span
                  className={cn(
                    "min-w-0 flex-1 truncate",
                    active && "font-medium",
                  )}
                >
                  {heading.subtitle ?? heading.label}
                </span>
                <ReadMark ratio={active ? undefined : ratio} />
              </Link>
            </li>
          );
        })}
      </ol>
    </SheetContent>
  );
}

/** A small ring for chapters started, a check for chapters finished. */
function ReadMark({ ratio }: { ratio?: number }) {
  if (ratio === undefined || ratio < 0.02) return null;
  if (ratio > 0.97) {
    return (
      <Check
        aria-label="已读完"
        className="size-3.5 shrink-0 text-muted-foreground"
      />
    );
  }
  const r = 5.5;
  const c = 2 * Math.PI * r;
  return (
    <svg
      viewBox="0 0 14 14"
      className="size-3.5 shrink-0 -rotate-90 text-muted-foreground"
      role="img"
      aria-label={`已读 ${Math.round(ratio * 100)}%`}
    >
      <circle
        cx="7"
        cy="7"
        r={r}
        fill="none"
        stroke="currentColor"
        strokeOpacity={0.25}
        strokeWidth={2}
      />
      <circle
        cx="7"
        cy="7"
        r={r}
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeDasharray={`${c * ratio} ${c}`}
        strokeLinecap="round"
      />
    </svg>
  );
}
