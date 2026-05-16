import { Link, useLocation } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Info, Monitor, Moon, Sun } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { getTheme, setTheme, type Theme } from "../lib/theme";

const nav = [
  { to: "/", label: "Library" },
  { to: "/import", label: "Import" },
  { to: "/settings", label: "Settings" },
];

const themeIcon: Record<Theme, typeof Sun> = {
  auto: Monitor,
  light: Sun,
  dark: Moon,
};

const themeLabel: Record<Theme, string> = {
  auto: "Auto",
  light: "Light",
  dark: "Dark",
};

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const isReader = location.pathname.startsWith("/r/");
  const [theme, setThemeState] = useState<Theme>(getTheme());

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const ThemeIcon = themeIcon[theme];

  return (
    <div className="min-h-svh">
      {!isReader && (
        <header className="sticky top-0 z-40 surface border-b border-border">
          <div className="mx-auto flex max-w-[1180px] items-center gap-6 px-5 py-3">
            <Link
              to="/"
              className="flex items-baseline gap-1 font-semibold tracking-tight"
            >
              <span className="text-[20px]">AO3</span>
              <span className="text-muted-foreground text-[14px]">Hub</span>
            </Link>
            <nav className="flex items-center gap-1 text-[13px] font-medium">
              {nav.map((n) => {
                const active = location.pathname === n.to;
                return (
                  <Link
                    key={n.to}
                    to={n.to}
                    className={cn(
                      "rounded-full px-3 py-1.5 transition-colors",
                      active
                        ? "bg-secondary text-foreground"
                        : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground",
                    )}
                  >
                    {n.label}
                  </Link>
                );
              })}
            </nav>
            <div className="ml-auto flex items-center gap-1">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="gap-1.5 px-2.5"
                    aria-label="切换主题"
                  >
                    <ThemeIcon className="size-3.5" />
                    <span>{themeLabel[theme]}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-[8rem]">
                  {(["auto", "light", "dark"] as Theme[]).map((t) => {
                    const Icon = themeIcon[t];
                    return (
                      <DropdownMenuItem
                        key={t}
                        onSelect={() => {
                          setTheme(t);
                          setThemeState(t);
                        }}
                      >
                        <Icon className="size-3.5" />
                        <span>{themeLabel[t]}</span>
                        {theme === t && (
                          <span className="ml-auto text-[11px] text-muted-foreground">
                            ●
                          </span>
                        )}
                      </DropdownMenuItem>
                    );
                  })}
                </DropdownMenuContent>
              </DropdownMenu>
              <Button variant="ghost" size="icon" asChild aria-label="版本">
                <Link to="/version">
                  <Info className="size-3.5" />
                </Link>
              </Button>
            </div>
          </div>
        </header>
      )}
      <main className={isReader ? "" : "mx-auto max-w-[1180px] px-5 py-10"}>
        {children}
      </main>
    </div>
  );
}
