import { Hono } from "hono";
import { cors } from "hono/cors";
import { logger } from "hono/logger";

import { HOST, PORT, VERSION } from "./env";
import stories from "./routes/stories";
import config from "./routes/config";
import stream from "./routes/stream";
import update from "./routes/update";
import auth from "./routes/auth";
import usersRoute from "./routes/users";
import { attachUser } from "./auth/middleware";
import { assets } from "./embedded";
import { initWorker, resumeOnStartup } from "./service";

const app = new Hono();
app.use(logger());
app.use("/api/*", cors());
app.use("/api/*", attachUser);

app.route("/api/auth", auth);
app.route("/api/users", usersRoute);
app.route("/api/stories", stories);
app.route("/api/stories", stream);
app.route("/api/config", config);
app.route("/api/update", update);
app.get("/api/health", (c) => c.json({ ok: true, version: VERSION }));

const MIME: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "application/javascript; charset=utf-8",
  ".mjs": "application/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".txt": "text/plain; charset=utf-8",
  ".map": "application/json; charset=utf-8",
};

function mimeFor(p: string): string {
  const i = p.lastIndexOf(".");
  if (i < 0) return "application/octet-stream";
  return MIME[p.slice(i).toLowerCase()] ?? "application/octet-stream";
}

app.get("*", (c) => {
  const url = new URL(c.req.url);
  let key = url.pathname === "/" ? "/index.html" : url.pathname;

  const direct = assets[key];
  if (direct) return direct();

  if (!key.includes(".") || key.endsWith(".html")) {
    const fallback = assets["/index.html"];
    if (fallback) return fallback();
  }

  if (Object.keys(assets).length === 0) {
    return c.text(
      "AO3-Hub server is running. Web bundle is not embedded — run `bun --cwd web run dev` or build the server.",
      404,
    );
  }
  return c.text("not found", 404);
});

initWorker();
await resumeOnStartup();

const server = Bun.serve({
  port: PORT,
  hostname: HOST,
  fetch: app.fetch,
});

console.log(`[ao3-hub] v${VERSION} listening on http://${server.hostname}:${server.port}`);

export default app;
export { MIME, mimeFor };
