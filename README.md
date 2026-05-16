# AO3-Hub

个人 AO3 翻译 + 阅读 CMS。粘 AO3 work URL 或拖 AO3「Download → HTML」导出文件进来 → 服务端解析 → LLM 段落级翻译 → 阅读器双语对照展示。单文件部署 + OTA 自更新。

## 形态

- **Bun + Hono** 服务端，**Vite + React + TanStack Router/Query + Tailwind v4** 前端
- 数据是分文件 JSON：`data/index.json` + `data/stories/{id}/{meta,original,translated,progress}.json` + `source.html`
- 翻译用 **OpenAI 兼容 `/chat/completions`**（DeepSeek/Claude/本地模型，用户自配 baseURL/apiKey/model）
- **段落级**翻译单元，可断点续传，可重试单段
- 进度推送走 **SSE**
- 最终产物是单个 `ao3-hub` 可执行文件，web 资源全部嵌入二进制
- **OTA**：远端 manifest URL 拉新版二进制，原子替换，由 launcher 重启

## 目录

```
AO3-Hub/
├── package.json            # workspaces
├── shared/schema.ts        # 前后端共享 zod schema
├── server/
│   ├── src/
│   │   ├── index.ts        # Hono + Bun.serve
│   │   ├── env.ts          # 路径/版本
│   │   ├── embedded.ts     # 生成的：web 资源 + 版本
│   │   ├── service.ts      # 业务编排
│   │   ├── update.ts       # OTA 逻辑
│   │   ├── queue.ts        # 内存队列
│   │   ├── sse.ts          # 事件总线
│   │   ├── ao3/{fetch,parse}.ts
│   │   ├── translate/{provider,prompt,chunker,worker}.ts
│   │   ├── db/{index,paths}.ts
│   │   └── routes/{stories,config,stream,update}.ts
│   └── scripts/{build.ts,launcher.sh}
└── web/
    ├── src/
    │   ├── main.tsx + router.tsx
    │   ├── pages/{Library,Import,Settings,Reader,Version,NotFound}.tsx
    │   ├── components/{AppLayout,StatusPill}.tsx
    │   ├── lib/{api,theme,reader-settings}.ts
    │   └── styles.css
    └── vite.config.ts
```

## Dev

需要 Bun ≥ 1.1.

```bash
bun install                   # 装所有 workspace
bun run dev                   # 并行起 server (:3000) + web (:5173)
```

或分别：

```bash
bun run dev:server            # 起 server，watch 模式
bun run dev:web               # 起 vite，:5173，/api 代理到 :3000
```

数据默认存 `./data/`，可用 `AO3HUB_DATA_DIR=/some/path` 覆盖。

## Build（单文件）

```bash
bun run build                 # vite build → 嵌入 → bun --compile
# 产物：server/build/ao3-hub  (~55 MB，含 Bun runtime)
```

跨平台构建（在 Mac 上构 Linux 二进制）：

```bash
AO3HUB_TARGET=bun-linux-x64 bun run build
AO3HUB_TARGET=bun-linux-arm64 bun run build
AO3HUB_TARGET=bun-darwin-arm64 bun run build
```

## CI / Release

GitHub Actions 已配置两条流水线：

- `CI`：push 到 `main` 或 PR 时执行 `bun install --frozen-lockfile`、类型检查和单文件构建。
- `Release`：push 形如 `v0.1.1` 的 tag 时构建 `darwin/linux` 的 `x64/arm64` 四个二进制，生成 `manifest.json` 和 `SHA256SUMS`，并发布到 GitHub Release。

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
PORT=3000 AO3HUB_DATA_DIR=/var/lib/ao3hub ./ao3-hub
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

## OTA

默认 manifest URL 指向 GitHub Release latest：

```text
https://github.com/x2146/AO3-Hub/releases/latest/download/manifest.json
```

Release workflow 会自动生成并上传 `manifest.json`。也可以手动发布一个兼容 manifest（参考 `manifest.example.json`）到任意 URL：

```json
{
  "version": "0.1.1",
  "assets": [
    { "platform": "darwin", "arch": "arm64", "url": "https://…/ao3-hub-darwin-arm64", "sha256": "…" },
    …
  ]
}
```

在 Settings 里确认 `Manifest URL`，然后到 `/version` 页面看是否有新版，点「下载并安装」：

1. server 拉对应平台的二进制
2. 校验 sha256（如果有）
3. 原子替换 `process.execPath`
4. `process.exit(0)`
5. launcher 检测到 exit 0，1 秒后重启新二进制

如果是 systemd 直接 ExecStart 二进制本身，可以加 `Restart=always` + `RestartSec=2` 来代替 launcher。

## 配置（`data/config.json`）

第一次启动会用默认值生成；通过 Settings 页改更顺手。

```jsonc
{
  "llm": {
    "baseURL": "https://api.deepseek.com/v1",
    "apiKey": "sk-…",
    "model": "deepseek-chat",
    "temperature": 0.3,
    "concurrency": 3,
    "blocksPerRequest": 8
  },
  "ao3": {
    "cookie": "_otwarchive_session=…",
    "userAgent": "Mozilla/5.0 …"
  },
  "reader": { "defaultMeasure": 760, "defaultFont": 17 },
  "update": {
    "manifestURL": "https://github.com/x2146/AO3-Hub/releases/latest/download/manifest.json",
    "channel": "stable",
    "autoCheck": false
  }
}
```

## API

| Method | Path | Body / 说明 |
|---|---|---|
| `GET` | `/api/health` | 健康检查 |
| `GET` | `/api/stories` | 书架索引 |
| `POST` | `/api/stories` | `{ url }`：服务端下载 + 解析 + 入队翻译 |
| `POST` | `/api/stories/upload` | multipart `file=<html>`：直接吃 AO3 导出 HTML |
| `GET` | `/api/stories/:id` | meta + progress |
| `GET` | `/api/stories/:id/chapters/:n` | 单章合并视图（原文 + 译文 + 状态） |
| `POST` | `/api/stories/:id/retry` | `{ blockIds?, chapterIndex? }` 重译 |
| `DELETE` | `/api/stories/:id` | 删除作品 + 数据 |
| `GET` | `/api/stories/:id/stream` | SSE：`progress` / `block-done` / `block-error` / `chapter-done` / `phase` |
| `GET` | `/api/config` | provider 配置（apiKey/cookie 脱敏） |
| `PUT` | `/api/config` | 更新配置 |
| `POST` | `/api/config/test` | 用当前配置 ping LLM |
| `GET` | `/api/update/version` | 当前 + manifest 信息 |
| `POST` | `/api/update/apply` | `{ force? }` 拉新版二进制 + 重启 |

## 翻译策略

- 段落（`<p>`/`<blockquote>` 等 `.userstuff` 直接子节点）= 翻译最小单元，id = `sha1(chapterIndex + html).slice(0,8)`
- 默认每批 8 段（约 3–4k tokens），并发 3
- system prompt 强制保留内联标签 / 段数 / 顺序，`response_format: json_object`
- 单段失败 → 标记 status=error，可在阅读器里逐段重试
- 整体进程崩溃 → 启动时扫 `progress.json` 非 ready/error 的故事重新入队，从首个 pending 段续跑

## 不做

用户系统、全文搜索、评论笔记、章节追更订阅。

## License

MIT
