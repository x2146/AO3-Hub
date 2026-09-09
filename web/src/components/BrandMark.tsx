import { cn } from "@/lib/utils";

/**
 * The app icon — the same glyph as `public/favicon.svg`. The tile and the
 * glyph are separate elements so the tile keeps its size inside containers
 * that force every `svg` to 16px (the sidebar menu button does).
 */
export function BrandMark({
  className,
  glyphClassName,
}: {
  className?: string;
  glyphClassName?: string;
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground",
        className,
      )}
    >
      <svg
        viewBox="14 14 36 36"
        fill="currentColor"
        className={cn("size-4", glyphClassName)}
      >
        <path d="M20.6 44 27.9 20h7.6L42.8 44h-6.2l-1.4-5.1h-7.8L26 44zm8-9.9h5.3l-2.6-9.4z" />
      </svg>
    </span>
  );
}
