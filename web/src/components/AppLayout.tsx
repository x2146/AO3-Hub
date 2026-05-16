import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import { Info, LogIn, LogOut, Monitor, Moon, Sun, UserCircle } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { getTheme, setTheme, type Theme } from "../lib/theme";
import { useAuth } from "../lib/auth";

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
  const navigate = useNavigate();
  const isReader = location.pathname.startsWith("/r/");
  const [theme, setThemeState] = useState<Theme>(getTheme());
  const { user, logout } = useAuth();

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const ThemeIcon = themeIcon[theme];

  const nav = useMemo(() => {
    const items: { to: string; label: string }[] = [{ to: "/", label: "Library" }];
    if (user) {
      items.push({ to: "/import", label: "Import" });
    }
    if (user?.role === "admin") {
      items.push({ to: "/users", label: "Users" });
      items.push({ to: "/settings", label: "Settings" });
    }
    return items;
  }, [user]);

  const onLogout = async () => {
    await logout();
    navigate({ to: "/", replace: true });
  };

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
              {user ? (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1.5 px-2.5"
                      aria-label="账号"
                    >
                      <UserCircle className="size-3.5" />
                      <span className="max-w-[120px] truncate">{user.username}</span>
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="min-w-[10rem]">
                    <DropdownMenuLabel>
                      {user.username}（{user.role}）
                    </DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onSelect={onLogout}>
                      <LogOut className="size-3.5" />
                      <span>登出</span>
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : (
                <Button variant="outline" size="sm" asChild className="gap-1.5">
                  <Link to="/login" search={{ redirect: undefined }}>
                    <LogIn className="size-3.5" />
                    登录
                  </Link>
                </Button>
              )}
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
