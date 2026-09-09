import { cn } from "@/lib/utils";

/**
 * The title block every top-level page opens with. Keeping the type scale and
 * the action slot in one place is what makes the pages read as one product —
 * a heading that is 2xl on the library and 3xl on settings would not.
 */
export function PageHeader({
  title,
  description,
  actions,
  className,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <header
      data-slot="page-header"
      className={cn(
        "flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between",
        className,
      )}
    >
      <div className="flex min-w-0 flex-col gap-1.5">
        <h1 className="cn-font-heading text-2xl font-semibold tracking-tight text-balance">
          {title}
        </h1>
        {description && (
          <p className="max-w-2xl text-sm text-pretty text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {actions && (
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {actions}
        </div>
      )}
    </header>
  );
}
