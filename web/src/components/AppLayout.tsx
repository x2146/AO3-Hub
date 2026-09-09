import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  BookOpenText,
  ChevronsUpDown,
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
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { Separator } from "@/components/ui/separator";
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
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { BrandMark } from "@/components/BrandMark";
import { applyTheme, getTheme, setTheme, type Theme } from "@/lib/theme";
import { useAuth } from "@/lib/auth";

const THEME_OPTIONS: { value: Theme; label: string; icon: LucideIcon }[] = [
  { value: "auto", label: "跟随系统", icon: Monitor },
  { value: "light", label: "浅色", icon: Sun },
  { value: "dark", label: "深色", icon: Moon },
];

const themeOption = (theme: Theme) =>
  THEME_OPTIONS.find((option) => option.value === theme) ?? THEME_OPTIONS[0];

type NavItem = {
  to: "/" | "/import" | "/users" | "/settings" | "/version";
  label: string;
  description: string;
  icon: LucideIcon;
};

const workspaceItems: NavItem[] = [
  { to: "/", label: "书架", description: "浏览与继续阅读", icon: BookOpenText },
  {
    to: "/import",
    label: "添加作品",
    description: "上传 HTML 或 AO3 链接",
    icon: FilePlus2,
  },
];

const adminItems: NavItem[] = [
  { to: "/users", label: "用户", description: "账号与权限", icon: UsersRound },
  {
    to: "/settings",
    label: "设置",
    description: "翻译与服务配置",
    icon: Settings2,
  },
];

const systemItems: NavItem[] = [
  { to: "/version", label: "版本", description: "更新与运行信息", icon: Info },
];

const PAGE_TITLE: Record<string, string> = {
  ...Object.fromEntries(
    [...workspaceItems, ...adminItems, ...systemItems].map((item) => [
      item.to,
      item.label,
    ]),
  ),
  "/login": "登录",
  "/setup": "初始化",
};

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const isReader = location.pathname.startsWith("/r/");
  const [theme, setThemeState] = useState<Theme>(getTheme());
  const { user, logout } = useAuth();

  useEffect(() => {
    applyTheme(theme);
    if (theme !== "auto") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const sync = () => applyTheme("auto");
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  }, [theme]);

  const changeTheme = (next: Theme) => {
    setTheme(next);
    setThemeState(next);
  };

  const onLogout = async () => {
    try {
      await logout();
      toast.success("已登出");
      navigate({ to: "/", replace: true });
    } catch (error) {
      toast.error("登出失败", {
        description: error instanceof Error ? error.message : undefined,
      });
    }
  };

  // The reader owns the entire viewport: no shell, no sidebar, no page chrome.
  if (isReader) {
    return (
      <TooltipProvider delayDuration={300}>
        <div className="min-h-svh">{children}</div>
      </TooltipProvider>
    );
  }

  const pageTitle = PAGE_TITLE[location.pathname];

  return (
    <TooltipProvider delayDuration={300}>
      <SidebarProvider defaultOpen>
        <AppSidebar pathname={location.pathname} onLogout={onLogout} />
        <SidebarInset className="min-w-0 overflow-hidden">
          <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b bg-background/90 px-4 backdrop-blur-md supports-backdrop-filter:bg-background/80 md:rounded-t-xl">
            <Tooltip>
              <TooltipTrigger asChild>
                <SidebarTrigger className="-ml-1.5" aria-label="切换侧栏" />
              </TooltipTrigger>
              <TooltipContent>
                切换侧栏
                <KbdGroup>
                  <Kbd>⌘</Kbd>
                  <Kbd>B</Kbd>
                </KbdGroup>
              </TooltipContent>
            </Tooltip>
            <Separator
              orientation="vertical"
              className="mx-1 data-vertical:h-4 data-vertical:self-center"
            />
            <Breadcrumb className="min-w-0">
              <BreadcrumbList className="flex-nowrap">
                <BreadcrumbItem className="hidden sm:inline-flex">
                  <BreadcrumbLink asChild>
                    <Link to="/">AO3 Hub</Link>
                  </BreadcrumbLink>
                </BreadcrumbItem>
                {pageTitle && (
                  <>
                    <BreadcrumbSeparator className="hidden sm:block" />
                    <BreadcrumbItem>
                      <BreadcrumbPage className="truncate">
                        {pageTitle}
                      </BreadcrumbPage>
                    </BreadcrumbItem>
                  </>
                )}
              </BreadcrumbList>
            </Breadcrumb>
            <div className="ml-auto flex items-center gap-1">
              <ThemeMenu theme={theme} onThemeChange={changeTheme} />
              {/* Desktop already offers this in the sidebar footer. */}
              {!user && location.pathname !== "/login" && (
                <Button
                  variant="outline"
                  size="sm"
                  className="md:hidden"
                  asChild
                >
                  <Link to="/login" search={{ redirect: undefined }}>
                    <LogIn data-icon="inline-start" />
                    登录
                  </Link>
                </Button>
              )}
            </div>
          </header>

          <div className="flex flex-1 flex-col">
            <div className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
              {children}
            </div>
          </div>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}

function ThemeMenu({
  theme,
  onThemeChange,
}: {
  theme: Theme;
  onThemeChange: (theme: Theme) => void;
}) {
  const current = themeOption(theme);
  const Icon = current.icon;
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`界面主题：${current.label}`}
            >
              <Icon />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>主题 · {current.label}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="min-w-40">
        <DropdownMenuGroup>
          <DropdownMenuLabel>界面主题</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={theme}
            onValueChange={(value) => onThemeChange(value as Theme)}
          >
            {THEME_OPTIONS.map((option) => (
              <DropdownMenuRadioItem key={option.value} value={option.value}>
                <option.icon />
                {option.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AppSidebar({
  pathname,
  onLogout,
}: {
  pathname: string;
  onLogout: () => void;
}) {
  const { user } = useAuth();
  const { setOpenMobile } = useSidebar();
  const workspace = user ? workspaceItems : workspaceItems.slice(0, 1);

  return (
    <Sidebar variant="inset" collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild tooltip="AO3 Hub">
              <Link to="/" onClick={() => setOpenMobile(false)}>
                <BrandMark />
                <div className="grid flex-1 text-left leading-tight">
                  <span className="truncate font-medium">AO3 Hub</span>
                  <span className="truncate text-xs text-sidebar-foreground/60">
                    双语阅读工作台
                  </span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavGroup label="工作区" items={workspace} pathname={pathname} />
        {user?.role === "admin" && (
          <NavGroup label="管理" items={adminItems} pathname={pathname} />
        )}
        <NavGroup label="系统" items={systemItems} pathname={pathname} />
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            {user ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <SidebarMenuButton size="lg" tooltip={user.username}>
                    <UserAvatar name={user.username} />
                    <div className="grid flex-1 text-left leading-tight">
                      <span className="truncate font-medium">
                        {user.username}
                      </span>
                      <span className="truncate text-xs text-sidebar-foreground/60">
                        {user.role === "admin" ? "管理员" : "普通用户"}
                      </span>
                    </div>
                    <ChevronsUpDown className="ml-auto" />
                  </SidebarMenuButton>
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  side="right"
                  align="end"
                  sideOffset={4}
                  className="min-w-52"
                >
                  <DropdownMenuLabel className="flex items-center gap-2 font-normal">
                    <UserAvatar name={user.username} />
                    <div className="grid flex-1 leading-tight">
                      <span className="truncate font-medium">
                        {user.username}
                      </span>
                      <span className="truncate text-xs text-muted-foreground">
                        {user.role === "admin" ? "管理员" : "普通用户"}
                      </span>
                    </div>
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuItem variant="destructive" onSelect={onLogout}>
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

/** Initial-letter avatar; accounts have no images. */
function UserAvatar({ name }: { name: string }) {
  return (
    <span
      aria-hidden
      className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-accent text-sm font-medium text-sidebar-accent-foreground uppercase"
    >
      {name.slice(0, 1)}
    </span>
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
