import type { RefObject } from "react";
import { Link } from "@tanstack/react-router";
import {
  Activity,
  ChevronLeft,
  ChevronRight,
  ListOrdered,
  Type,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ReaderView } from "@/lib/reader-settings";

export const VIEW_OPTIONS: {
  value: ReaderView;
  label: string;
  short: string;
  hint: string;
}[] = [
  {
    value: "bilingual",
    label: "双语",
    short: "双语",
    hint: "原文在上，译文在下",
  },
  { value: "zh", label: "仅中文", short: "中", hint: "只显示译文" },
  { value: "en", label: "仅原文", short: "EN", hint: "只显示英文原文" },
];

/**
 * A navigation bar in the native sense: translucent, hairline-bordered once
 * the page moves under it, and the work title fades in only after the big
 * on-page title has scrolled away.
 */
export function ReaderTopbar(props: {
  visible: boolean;
  scrolled: boolean;
  showTitle: boolean;
  title: string;
  subtitle: string;
  onOpenStatus: () => void;
  statusTriggerRef: RefObject<HTMLButtonElement>;
}) {
  return (
    <header
      className={cn(
        "fixed inset-x-0 top-0 z-40 border-b pt-[env(safe-area-inset-top)] transition-[transform,background-color,border-color] duration-300 ease-out",
        props.scrolled
          ? "border-border/70 bg-background/80 backdrop-blur-xl backdrop-saturate-150"
          : "border-transparent bg-background/0",
        props.visible ? "translate-y-0" : "-translate-y-full",
      )}
    >
      <div className="mx-auto grid h-12 w-full max-w-5xl grid-cols-[1fr_auto_1fr] items-center gap-2 px-2 sm:px-3">
        <div className="flex justify-start">
          <Button variant="ghost" size="sm" className="-ml-1 gap-0.5" asChild>
            <Link to="/" aria-label="返回书架">
              <ChevronLeft data-icon="inline-start" />
              <span className="hidden sm:inline">书架</span>
            </Link>
          </Button>
        </div>

        <div
          className={cn(
            "min-w-0 max-w-[min(60vw,32rem)] text-center transition-opacity duration-300",
            props.showTitle ? "opacity-100" : "pointer-events-none opacity-0",
          )}
          aria-hidden={!props.showTitle}
        >
          <p className="truncate text-sm leading-tight font-medium">
            {props.title}
          </p>
          <p className="truncate text-[11px] leading-tight text-muted-foreground tabular-nums">
            {props.subtitle}
          </p>
        </div>

        <div className="flex justify-end">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                ref={props.statusTriggerRef}
                variant="ghost"
                size="icon-sm"
                onClick={props.onOpenStatus}
                aria-label="翻译状态"
              >
                <Activity />
              </Button>
            </TooltipTrigger>
            <TooltipContent>翻译状态</TooltipContent>
          </Tooltip>
        </div>
      </div>
    </header>
  );
}

/**
 * The floating control capsule at the bottom edge — where a thumb rests on a
 * phone, and out of the text's way on a desktop. It slides away with the top
 * bar while reading.
 */
export function ReaderDock(props: {
  visible: boolean;
  onPrev?: () => void;
  onNext?: () => void;
  view: ReaderView;
  onViewChange: (view: ReaderView) => void;
  showToc: boolean;
  tocOpen: boolean;
  tocTriggerRef: RefObject<HTMLButtonElement>;
  onToggleToc: () => void;
  settingsOpen: boolean;
  settingsTriggerRef: RefObject<HTMLButtonElement>;
  onToggleSettings: () => void;
}) {
  return (
    <div
      className={cn(
        "pointer-events-none fixed inset-x-0 bottom-0 z-40 flex justify-center px-3 pb-[max(1rem,env(safe-area-inset-bottom))] transition-[transform,opacity] duration-300 ease-out",
        props.visible
          ? "translate-y-0 opacity-100"
          : "translate-y-[calc(100%+1rem)] opacity-0",
      )}
    >
      <nav
        aria-label="阅读控制"
        className="pointer-events-auto flex items-center gap-0.5 rounded-full border border-border/70 bg-popover/80 p-1 text-popover-foreground shadow-[0_8px_30px_-12px_rgb(0_0_0/0.35)] backdrop-blur-xl backdrop-saturate-150"
      >
        <DockButton
          label="上一章"
          kbd="←"
          onClick={props.onPrev}
          disabled={!props.onPrev}
        >
          <ChevronLeft />
        </DockButton>
        {props.showToc && (
          <DockButton
            label="章节目录"
            kbd="T"
            triggerRef={props.tocTriggerRef}
            active={props.tocOpen}
            onClick={props.onToggleToc}
          >
            <ListOrdered />
          </DockButton>
        )}

        <ToggleGroup
          type="single"
          value={props.view}
          onValueChange={(value) =>
            value && props.onViewChange(value as ReaderView)
          }
          size="sm"
          spacing={0}
          className="mx-0.5 rounded-full bg-muted/70 p-0.5"
          aria-label="显示语言"
        >
          {VIEW_OPTIONS.map((option) => (
            <ToggleGroupItem
              key={option.value}
              value={option.value}
              aria-label={option.label}
              title={option.hint}
              className="h-7 min-w-9 rounded-full! px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm"
            >
              {option.short}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>

        <DockButton
          label="阅读设置"
          kbd="S"
          triggerRef={props.settingsTriggerRef}
          active={props.settingsOpen}
          onClick={props.onToggleSettings}
        >
          <Type />
        </DockButton>
        <DockButton
          label="下一章"
          kbd="→"
          onClick={props.onNext}
          disabled={!props.onNext}
        >
          <ChevronRight />
        </DockButton>
      </nav>
    </div>
  );
}

function DockButton({
  label,
  kbd,
  onClick,
  disabled,
  active,
  triggerRef,
  children,
}: {
  label: string;
  kbd?: string;
  onClick?: () => void;
  disabled?: boolean;
  active?: boolean;
  triggerRef?: RefObject<HTMLButtonElement>;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          ref={triggerRef}
          variant={active ? "secondary" : "ghost"}
          size="icon"
          className="size-9 rounded-full"
          onClick={onClick}
          disabled={disabled}
          aria-label={label}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="top" className="flex items-center gap-1.5">
        {label}
        {kbd && <Kbd>{kbd}</Kbd>}
      </TooltipContent>
    </Tooltip>
  );
}
