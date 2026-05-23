# AO3-Hub

个人 AO3 翻译 + 阅读 CMS。粘 AO3 work URL 或拖 AO3「Download → HTML」导出文件进来 → 服务端解析 → LLM 段落级翻译 → 阅读器双语对照展示。单文件部署 + OTA 自更新。

## 形态

- **Go** 服务端，**Vite + React + TanStack Router/Query + Tailwind v4** 前端
- 数据是分文件 JSON：`data/index.json` + `data/stories/{id}/{meta,original,translated,progress}.json` + `source.html`；用户与 session 走 `data/users.json` + `data/sessions.json`
- 翻译支持 **OpenAI 兼容 `/chat/completions`** 与 **Claude Messages**（用户自配 apiType/baseURL/apiKey/model）
- **段落级**翻译单元，可断点续传，可重试单段
- 进度推送走 **SSE**
- **多用户**：匿名可读 + user 可写 + admin 管设置与用户，密码 argon2id，cookie session
- 最终产物是单个 `ao3-hub` 可执行文件，web 资源全部嵌入二进制
- **OTA**：远端 manifest URL 拉新版二进制，原子替换，由 launcher 重启

## 目录

```
AO3-Hub/
├── package.json            # workspaces
├── shared/schema.ts        # 前端 TypeScript 类型/schema
├── cmd/ao3hub/main.go      # Go 服务入口
├── internal/
│   ├── app/                # Go 后端：路由、数据、认证、AO3、翻译、OTA
│   └── webassets/          # go:embed 前端产物
├── server/
│   └── scripts/launcher.sh # OTA 重启 launcher
├── scripts/build-go.mjs    # vite build → go:embed → go build
├── cmd/ao3hub-manifest/    # OTA manifest 生成器
└── web/
    ├── src/
    │   ├── main.tsx + router.tsx
    │   ├── pages/{Library,Import,Settings,Reader,Version,Login,Setup,Users,NotFound}.tsx
    │   ├── components/{AppLayout,StatusPill}.tsx
    │   ├── lib/{api,auth,theme,reader-settings}.ts
    │   └── styles.css
    └── vite.config.ts
```

## Dev

需要 Go 和 Node.js/npm（前端构建/包管理）。

```bash
npm install                   # 装所有 workspace
npm run dev                   # 并行起 server（默认 :3000）+ web (:5173)
```

或分别：

```bash
npm run dev:server            # 起 Go server
npm run dev:web               # 起 vite，:5173，/api 代理到配置的 server 端口
```

数据默认存 `./data/`，可用 `AO3HUB_DATA_DIR=/some/path` 覆盖。
监听地址默认从 `data/config.json` 的 `server.host` / `server.port` 读取；
启动时也可用 `HOST` / `PORT` 临时覆盖。

## Build（单文件）

```bash
npm run build                 # vite build → go:embed → go build
# 产物：server/build/ao3-hub
```

跨平台构建（在 Mac 上构 Linux 二进制）：

```bash
AO3HUB_TARGET=linux-x64 npm run build
AO3HUB_TARGET=linux-arm64 npm run build
AO3HUB_TARGET=darwin-arm64 npm run build
```

## CI / Release

GitHub Actions 使用同一个 `CI / Release` workflow：

- PR：只执行 `npm ci`、类型检查和单文件构建。
- 普通 push：先执行 CI 校验，通过后发布滚动 `dev` OTA。
- push 形如 `v0.1.1` 的 tag：先执行 CI 校验，通过后发布稳定版 OTA。

Release 使用 matrix 交叉编译四个目标：

- `darwin-arm64` → `ao3-hub-darwin-arm64`
- `darwin-x64` → `ao3-hub-darwin-x64`
- `linux-arm64` → `ao3-hub-linux-arm64`
- `linux-x64` → `ao3-hub-linux-x64`

普通 push 会覆盖 GitHub prerelease `dev` 的资源，但每次构建都有唯一版本号：

```text
dev-<run_number:0000>-<UTC yyyyMMdd>-<short_sha>
```

这个版本号会同时写入二进制和 OTA `manifest.json`。例如启动日志会显示：

```text
[ao3-hub] dev-0012-20260523-abcdef0 listening on http://0.0.0.0:3000
```

dev channel manifest 固定在：

```text
https://github.com/x2146/AO3-Hub/releases/download/dev/manifest.json
```

发版流程：

```bash
# 先把 package.json 里的 version 改到目标版本，例如 0.1.1
git tag v0.1.1
git push origin v0.1.1
```

tag 必须匹配 `package.json` 的 version（允许 `v` 前缀）。Release 完成后，OTA manifest 会在：

```text
https://github.com/x2146/AO3-Hub/releases/latest/download/manifest.json
```

## Run（生产）

直接跑：

```bash
AO3HUB_DATA_DIR=/var/lib/ao3hub ./ao3-hub
# 临时覆盖监听端口：
PORT=3001 AO3HUB_DATA_DIR=/var/lib/ao3hub ./ao3-hub
```

通过 launcher（推荐，OTA 升级后自动重启）：

```bash
cp server/scripts/launcher.sh ./launcher.sh
./launcher.sh
```

systemd unit：

```ini
[Service]
WorkingDirectory=/opt/ao3-hub
ExecStart=/opt/ao3-hub/launcher.sh
Environment=AO3HUB_DATA_DIR=/var/lib/ao3hub
Restart=on-failure
```

反代由 caddy/nginx 处理 TLS。

## 用户与权限

三层权限：

| 角色 | 权限 |
|---|---|
| 匿名 | 读 Library / Reader / SSE 进度流 / 版本 |
| `user` | 以上 + 导入、上传、删除、重试翻译 |
| `admin` | 以上 + 用户管理（`/users`）、系统设置（`/settings`）、OTA 应用 |

首次启动 `data/users.json` 不存在时，前端会跳到 `/setup` 让你创建首个 admin。之后在 `/users` 页可以新建 / 删除用户，或重置密码（重置会清掉该用户所有 session）。

- 密码用 Go 的 argon2id 哈希（兼容 PHC 字符串格式）
- session = 32 字节随机 token，存 `data/sessions.json`，以 HttpOnly + SameSite=Lax cookie 下发；反代为 HTTPS 时自动加 `Secure`
- 滑动过期 30 天：每次请求 touch 后顺延
- 删用户 / 重置密码会清空对应 session
- 最后一个 admin 不能被删除或降级，自己不能改自己的角色 / 删自己

## OTA

默认 stable channel manifest URL 指向 GitHub Release latest：

```text
https://github.com/x2146/AO3-Hub/releases/latest/download/manifest.json
```

Release workflow 会自动用 Go 生成并上传 `manifest.json`。也可以手动发布一个兼容 manifest（参考 `manifest.example.json`）到任意 URL：

```json
{
  "version": "0.1.1",
  "channel": "stable",
  "assets": [
    { "platform": "darwin", "arch": "arm64", "url": "https://…/ao3-hub-darwin-arm64", "sha256": "…" },
    …
  ]
}
```

在 Settings 里选择 `stable` 或 `dev` channel，确认 `Manifest URL`，然后到 `/version` 页面看是否有新版，点「下载并安装」：

1. server 拉对应平台的二进制
2. 校验 sha256（如果有）
3. 原子替换 `process.execPath`
4. `process.exit(0)`
5. launcher 检测到 exit 0，1 秒后重启新二进制

如果要重新拉取 manifest 中显示的远端版本，即使当前版本号相同，也可以点「强制拉取此版本」。API 可传：

```json
{ "force": true, "forceVersion": "dev-0012-20260523-abcdef0" }
```

如果是 systemd 直接 ExecStart 二进制本身，可以加 `Restart=always` + `RestartSec=2` 来代替 launcher。

## 配置（`data/config.json`）

第一次启动会用默认值生成；通过 Settings 页改更顺手。

```jsonc
{
  "server": {
    "host": "0.0.0.0",
    "port": 3000
  },
  "auth": {
    "sessionTtlDays": 30
  },
  "stream": {
    "heartbeatMs": 15000
  },
  "import": {
    "minHtmlLength": 100
  },
  "ui": {
    "libraryRefetchIntervalMs": 3000
  },
  "llm": {
    "apiType": "openai-compatible",
    "baseURL": "https://api.deepseek.com/v1",
    "apiKey": "sk-…",
    "model": "deepseek-chat",
    "temperature": 0.3,
    "concurrency": 3,
    "blocksPerRequest": 8,
    "maxTokensPerRequest": 3500
  },
  "ao3": {
    "cookie": "_otwarchive_session=…",
    "userAgent": "Mozilla/5.0 …"
  },
  "reader": {
    "defaultMeasure": 780,
    "defaultFont": 17,
    "defaultZhScale": 0.96
  },
  "update": {
    "manifestURL": "https://github.com/x2146/AO3-Hub/releases/latest/download/manifest.json",
    "channel": "stable",
    "autoCheck": false,
    "restartDelayMs": 600
  }
}
```

## API

> 权限标记：⚪ 匿名可访问 · 🔑 需登录 · 👑 admin only。未标即匿名可访问。

| Method | Path | 权限 | Body / 说明 |
|---|---|---|---|
| `GET` | `/api/health` | ⚪ | 健康检查 |
| `GET` | `/api/auth/me` | ⚪ | 当前用户 + 是否需要初始化 |
| `GET` | `/api/auth/setup-status` | ⚪ | `{ needsSetup }` |
| `POST` | `/api/auth/setup` | ⚪ | `{ username, password }`：仅在无用户时可用，创建首个 admin |
| `POST` | `/api/auth/login` | ⚪ | `{ username, password }`，成功后下发 cookie |
| `POST` | `/api/auth/logout` | ⚪ | 清掉当前 session |
| `GET` | `/api/users` | 👑 | 用户列表 |
| `POST` | `/api/users` | 👑 | `{ username, password, role }` 创建用户 |
| `PUT` | `/api/users/:id` | 👑 | `{ password?, role? }` 改密 / 改角色 |
| `DELETE` | `/api/users/:id` | 👑 | 删除用户（不能删自己 / 最后一个 admin） |
| `GET` | `/api/stories` | ⚪ | 书架索引 |
| `POST` | `/api/stories` | 🔑 | `{ url }`：服务端下载 + 解析 + 入队翻译 |
| `POST` | `/api/stories/upload` | 🔑 | multipart `file=<html>`：直接吃 AO3 导出 HTML |
| `GET` | `/api/stories/:id` | ⚪ | meta + progress |
| `GET` | `/api/stories/:id/chapters/:n` | ⚪ | 单章合并视图（原文 + 译文 + 状态） |
| `POST` | `/api/stories/:id/retry` | 🔑 | `{ blockIds?, chapterIndex? }` 重译 |
| `DELETE` | `/api/stories/:id` | 🔑 | 删除作品 + 数据 |
| `GET` | `/api/stories/:id/stream` | ⚪ | SSE：`progress` / `block-done` / `block-error` / `chapter-done` / `phase` |
| `GET` | `/api/config/public` | ⚪ | 公开运行配置（reader/ui） |
| `GET` | `/api/config` | 👑 | provider 配置（apiKey/cookie 脱敏） |
| `PUT` | `/api/config` | 👑 | 更新配置 |
| `POST` | `/api/config/test` | 👑 | 用当前配置 ping LLM |
| `GET` | `/api/update/version` | ⚪ | 当前 + manifest 信息 |
| `POST` | `/api/update/check` | 👑 | 重新拉一次 manifest |
| `POST` | `/api/update/apply` | 👑 | `{ force?, forceVersion? }` 拉新版二进制 + 重启 |

## 翻译策略

- 段落（`<p>`/`<blockquote>` 等 `.userstuff` 直接子节点）= 翻译最小单元，id = `sha1(chapterIndex + html).slice(0,8)`
- 默认每批 8 段（约 3–4k tokens），并发 3
- system prompt 强制保留内联标签 / 段数 / 顺序；OpenAI 兼容接口使用 `response_format: json_object`，Claude Messages 使用 system JSON 指令
- 单段失败 → 标记 status=error，可在阅读器里逐段重试
- 整体进程崩溃 → 启动时扫 `progress.json` 非 ready/error 的故事重新入队，从首个 pending 段续跑

## 不做

全文搜索、评论笔记、章节追更订阅。

## License

MIT
