import { useEffect, useState } from "react"
import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon,
} from "lucide-react"
import { Toaster as Sonner, type ToasterProps } from "sonner"

import { getTheme, type Theme } from "@/lib/theme"

// The registry version reads the theme from next-themes; this app owns its own
// tiny theme store (lib/theme.ts) and toggles `.dark` on <html>, so the toaster
// mirrors that instead.
function useResolvedTheme(): "light" | "dark" {
  const [theme, setThemeState] = useState<Theme>(() => getTheme())
  const [systemDark, setSystemDark] = useState(
    () => window.matchMedia("(prefers-color-scheme: dark)").matches,
  )

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)")
    const onSystemChange = () => setSystemDark(media.matches)
    media.addEventListener("change", onSystemChange)

    // lib/theme.ts writes the resolved mode onto <html>, so observing the
    // element keeps the toaster in sync no matter who changed the theme.
    const observer = new MutationObserver(() => setThemeState(getTheme()))
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "data-theme"],
    })

    return () => {
      media.removeEventListener("change", onSystemChange)
      observer.disconnect()
    }
  }, [])

  if (theme === "auto") return systemDark ? "dark" : "light"
  return theme
}

function Toaster({ ...props }: ToasterProps) {
  const theme = useResolvedTheme()

  return (
    <Sonner
      theme={theme}
      className="toaster group"
      position="bottom-right"
      closeButton
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--success-text": "var(--success)",
          "--error-text": "var(--destructive)",
          "--border-radius": "var(--radius)",
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }
