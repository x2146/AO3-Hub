import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import {
  BookOpenText,
  Check,
  ChevronUp,
  CircleUserRound,
  FilePlus2,
  Info,
  LogIn,
  LogOut,
  Monitor,
  Moon,
  Settings2,
  Sun,
  UsersRound,
  type LucideIcon,
} from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { applyTheme, getTheme, setTheme, type Theme } from "@/lib/theme";
import { useAuth } from "@/lib/auth";

const themeIcon: Record<Theme, LucideIcon> = {
  auto: Monitor,
  light: Sun,
  dark: Moon,
};

const themeLabel: Record<Theme, string> = {
  auto: "跟随系统",
  light: "浅色",
  dark: "深色",
};

type NavItem = {
  to: "/" | "/import" | "/users" | "/settings" | "/version";
  label: string;
  description: string;
  icon: LucideIcon;
};

const workspaceItems: NavItem[] = [
  { to: "/", label: "书架", description: "浏览与继续阅读", icon: BookOpenText },
  { to: "/import", label: "添加作品", description: "上传 HTML 或 AO3 链接", icon: FilePlus2 },
];

const adminItems: NavItem[] = [
  { to: "/users", label: "用户", description: "账号与权限", icon: UsersRound },
  { to: "/settings", label: "设置", description: "翻译与服务配置", icon: Settings2 },
];

const systemItems: NavItem[] = [
  { to: "/version", label: "版本", description: "更新与运行信息", icon: Info },
];

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const isReader = location.pathname.startsWith("/r/");
  const [theme, setThemeState] = useState<Theme>(getTheme());
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const { user, logout } = useAuth();

  useEffect(() => {
    applyTheme(theme);
    if (theme !== "auto") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const sync = () => applyTheme("auto");
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  }, [theme]);

  const pageTitle = useMemo(() => {
    const all = [...workspaceItems, ...adminItems, ...systemItems];
    return all.find((item) => item.to === location.pathname)?.label ??
      (location.pathname === "/login"
        ? "登录"
        : location.pathname === "/setup"
          ? "初始化"
          : "AO3 Hub");
  }, [location.pathname]);

  const onLogout = async () => {
    setLogoutError(null);
    try {
      await logout();
      navigate({ to: "/", replace: true });
    } catch (error) {
      setLogoutError(error instanceof Error ? error.message : "登出失败");
    }
  };

  if (isReader) {
    return (
      <TooltipProvider>
        <div className="min-h-svh">{children}</div>
      </TooltipProvider>
    );
  }

  return (
    <TooltipProvider>
      <SidebarProvider defaultOpen>
        <AppSidebar
          pathname={location.pathname}
          theme={theme}
          onThemeChange={(next) => {
            setTheme(next);
            setThemeState(next);
          }}
          onLogout={onLogout}
        />
        <SidebarInset className="min-w-0 bg-background/70">
          <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-3 border-b bg-background/85 px-4 backdrop-blur-xl sm:px-6">
            <SidebarTrigger aria-label="切换侧栏" />
            <div className="h-5 w-px bg-border" aria-hidden />
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold">{pageTitle}</p>
              <p className="hidden text-xs text-muted-foreground sm:block">
                你的本地 AO3 阅读与翻译工作台
              </p>
            </div>
            <div className="ml-auto flex items-center gap-1.5">
              {user && location.pathname !== "/import" && (
                <Button size="sm" asChild>
                  <Link to="/import">
                    <FilePlus2 data-icon="inline-start" />
                    <span className="hidden sm:inline">添加作品</span>
                  </Link>
                </Button>
              )}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`主题：${themeLabel[theme]}`}
                    onClick={() => {
                      const next: Theme =
                        theme === "auto"
                          ? "light"
                          : theme === "light"
                            ? "dark"
                            : "auto";
                      setTheme(next);
                      setThemeState(next);
                    }}
                  >
                    {(() => {
                      const ThemeIcon = themeIcon[theme];
                      return <ThemeIcon data-icon="inline-start" />;
                    })()}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{themeLabel[theme]}</TooltipContent>
              </Tooltip>
              {!user && location.pathname !== "/login" && (
                <Button variant="outline" size="sm" asChild>
                  <Link to="/login" search={{ redirect: undefined }}>
                    <LogIn data-icon="inline-start" />
                    登录
                  </Link>
                </Button>
              )}
            </div>
          </header>

          <div className="flex flex-1 flex-col">
            {logoutError && (
              <div className="mx-auto w-full max-w-[1200px] px-4 pt-6 sm:px-6 lg:px-8">
                <Alert variant="destructive">
                  <LogOut />
                  <AlertTitle>登出失败</AlertTitle>
                  <AlertDescription>{logoutError}</AlertDescription>
                </Alert>
              </div>
            )}
            <div className="mx-auto w-full max-w-[1200px] flex-1 px-4 py-6 sm:px-6 sm:py-8 lg:px-8 lg:py-10">
              {children}
            </div>
          </div>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}

function AppSidebar({
  pathname,
  theme,
  onThemeChange,
  onLogout,
}: {
  pathname: string;
  theme: Theme;
  onThemeChange: (theme: Theme) => void;
  onLogout: () => void;
}) {
  const { user } = useAuth();
  const { setOpenMobile } = useSidebar();
  const workspace = user ? workspaceItems : workspaceItems.slice(0, 1);

  return (
    <Sidebar variant="inset" collapsible="icon">
      <SidebarHeader className="p-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild tooltip="AO3 Hub">
              <Link to="/" onClick={() => setOpenMobile(false)}>
                <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sm font-bold text-sidebar-primary-foreground shadow-sm">
                  A3
                </div>
                <div className="grid flex-1 text-left leading-tight">
                  <span className="truncate font-semibold">AO3 Hub</span>
                  <span className="truncate text-xs text-sidebar-foreground/65">
                    Local reading studio
                  </span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarSeparator />
      <SidebarContent>
        <NavGroup label="工作区" items={workspace} pathname={pathname} />
        {user?.role === "admin" && (
          <NavGroup label="管理" items={adminItems} pathname={pathname} />
        )}
        <NavGroup label="系统" items={systemItems} pathname={pathname} />
      </SidebarContent>
      <SidebarSeparator />
      <SidebarFooter className="p-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <SidebarMenuButton tooltip="切换主题">
                  {(() => {
                    const ThemeIcon = themeIcon[theme];
                    return <ThemeIcon />;
                  })()}
                  <span>{themeLabel[theme]}</span>
                  <ChevronUp className="ml-auto" />
                </SidebarMenuButton>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="right" align="end" className="min-w-40">
                <DropdownMenuGroup>
                  <DropdownMenuLabel>界面主题</DropdownMenuLabel>
                  {(["auto", "light", "dark"] as Theme[]).map((item) => {
                    const Icon = themeIcon[item];
                    return (
                      <DropdownMenuItem key={item} onSelect={() => onThemeChange(item)}>
                        <Icon />
                        <span>{themeLabel[item]}</span>
                        {theme === item && <Check className="ml-auto" />}
                      </DropdownMenuItem>
                    );
                  })}
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
          <SidebarMenuItem>
            {user ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <SidebarMenuButton size="lg" tooltip={user.username}>
                    <CircleUserRound />
                    <div className="grid flex-1 text-left leading-tight">
                      <span className="truncate font-medium">{user.username}</span>
                      <span className="truncate text-xs text-sidebar-foreground/65">
                        {user.role === "admin" ? "管理员" : "用户"}
                      </span>
                    </div>
                    <ChevronUp className="ml-auto" />
                  </SidebarMenuButton>
                </DropdownMenuTrigger>
                <DropdownMenuContent side="right" align="end" className="min-w-48">
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>{user.username}</DropdownMenuLabel>
                    <DropdownMenuItem onSelect={onLogout}>
                      <LogOut />
                      登出
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : (
              <SidebarMenuButton asChild tooltip="登录">
                <Link
                  to="/login"
                  search={{ redirect: undefined }}
                  onClick={() => setOpenMobile(false)}
                >
                  <LogIn />
                  <span>登录</span>
                </Link>
              </SidebarMenuButton>
            )}
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

function NavGroup({
  label,
  items,
  pathname,
}: {
  label: string;
  items: NavItem[];
  pathname: string;
}) {
  const { setOpenMobile } = useSidebar();
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{label}</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => (
            <SidebarMenuItem key={item.to}>
              <SidebarMenuButton
                asChild
                isActive={pathname === item.to}
                tooltip={item.description}
              >
                <Link to={item.to} onClick={() => setOpenMobile(false)}>
                  <item.icon />
                  <span>{item.label}</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}
