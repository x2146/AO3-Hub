import { Link, useLocation } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { cycleTheme, getTheme } from "../lib/theme";

const nav = [
  { to: "/", label: "Library" },
  { to: "/import", label: "Import" },
  { to: "/settings", label: "Settings" },
];

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const isReader = location.pathname.startsWith("/r/");
  const [theme, setTheme] = useState(getTheme());

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  return (
    <div className="min-h-svh">
      {!isReader && (
        <header className="sticky top-0 z-40 surface border-b rule">
          <div className="mx-auto flex max-w-[1180px] items-center gap-6 px-5 py-3">
            <Link
              to="/"
              className="flex items-baseline gap-1 font-semibold tracking-tight"
            >
              <span className="text-[20px]">AO3</span>
              <span className="text-muted text-[14px]">Hub</span>
            </Link>
            <nav className="flex items-center gap-1 text-[13px] font-medium">
              {nav.map((n) => {
                const active = location.pathname === n.to;
                return (
                  <Link
                    key={n.to}
                    to={n.to}
                    className={`rounded-full px-3 py-1.5 transition-colors ${
                      active
                        ? "bg-[rgb(var(--ink)/0.08)] text-[rgb(var(--ink))]"
                        : "text-muted hover:bg-[rgb(var(--ink)/0.05)] hover:text-[rgb(var(--ink))]"
                    }`}
                  >
                    {n.label}
                  </Link>
                );
              })}
            </nav>
            <div className="ml-auto flex items-center gap-2">
              <button
                type="button"
                className="btn btn-ghost text-[12px]"
                onClick={() => setTheme(cycleTheme())}
                aria-label="切换主题"
                title="切换主题"
              >
                {theme === "auto" ? "Auto" : theme === "light" ? "Light" : "Dark"}
              </button>
              <Link to="/version" className="btn btn-ghost text-[12px]">
                ⓥ
              </Link>
            </div>
          </div>
        </header>
      )}
      <main className={isReader ? "" : "mx-auto max-w-[1180px] px-5 py-10"}>{children}</main>
    </div>
  );
}
