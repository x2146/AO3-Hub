# x2146-fic — 技术设计

个人 AO3 翻译 + 阅读 CMS。贴 AO3 链接 → 服务端下载 → LLM 翻译 → 入库 → 阅读器展示。

## 背景

当前工作流（保留作参考）：
1. 浏览器 `immersive-translate` 翻译 AO3 页面，导出 `*-zh-CN-dual.html`
2. 本地跑 `node build-reader-pages.mjs` 套上 reader chrome，生成 `*-reader.html` + `list.html`
3. 手动打开 `list.html` 阅读

痛点：手动步骤多、翻译质量受限于 immersive-translate、reader 是静态 HTML（没法做进度同步/搜索/标签）。

目标：保持现有的阅读器观感和双语段落对照形态，把流水线后移到一个常驻服务里。

## 技术栈

| 层 | 选型 | 理由 |
|---|---|---|
| 运行时 | **Bun** | 原生 TS、内置 fetch、单进程长驻 |
| 后端框架 | **Hono** | 极简 REST，类型友好，无 SSR 概念 |
| 持久化 | **lowdb v7**（async）+ 分文件 JSON | 个人量级足够，可读可手改 |
| HTML 解析 | **cheerio** | AO3 HTML 结构规整，jQuery API 顺手 |
| 翻译 | **OpenAI 兼容 `/chat/completions`** | 用户自配 `baseURL/apiKey/model`，DeepSeek/Claude/本地模型通吃 |
| 任务队列 | **内存队列** + 持久化状态 | 单进程不需要 Redis；服务重启时根据 `status` 字段续跑 |
| 进度推送 | **SSE**（`text/event-stream`） | 半双工足够；不想用就降级到 2s 轮询 |
| 前端构建 | **Vite + React + TS** | 纯 SPA，与后端解耦 |
| UI | **shadcn/ui + Tailwind v4** | 沿用现有 `#018eee` accent 和阅读器美学 |
| 路由 | **TanStack Router**（文件式，类型安全） | 比 React Router 强 |
| 数据层 | **TanStack Query** | 缓存/轮询/SSE 一把梭 |
| 校验 | **zod**，`shared/` 目录前后端共用 | |
| 部署 | Bun 进程 + caddy/nginx 反代，systemd 守护 | |

## 数据模型

JSON 分文件，元数据轻索引，章节内容懒加载。

```
server/data/
├── config.json                 # LLM provider 配置（gitignore）
├── index.json                  # 故事索引，书架页用
└── stories/
    └── {storyId}/              # storyId = AO3 work id（如 12345678）
        ├── meta.json           # 故事元信息
        ├── source.html         # AO3 原始 HTML 存档（出错可重解析）
        ├── original.json       # 原文章节
        ├── translated.json     # 译文章节（按章节增量写）
        └── progress.json       # 翻译进度 + 错误日志
```

### `index.json`

```ts
type Index = {
  stories: Array<{
    id: string                    // AO3 work id
    title: string
    chineseTitle?: string
    author: string
    chapterCount: number
    wordCount: number
    status: "queued" | "fetching" | "translating" | "ready" | "error"
    addedAt: string               // ISO
    updatedAt: string
  }>
}
```

### `stories/{id}/meta.json`

```ts
type Meta = {
  id: string
  url: string                     // AO3 work URL
  downloadUrl: string             // AO3 HTML download URL
  title: string
  chineseTitle?: string
  author: string
  authorUrl?: string
  summary?: string
  tags: {
    fandom: string[]
    relationship: string[]
    character: string[]
    additional: string[]
    rating?: string
    warnings?: string[]
    categories?: string[]
  }
  language: string                // 原文语言，通常 "en"
  publishedAt?: string
  updatedAt?: string
  wordCount: number
  chapterCount: number
}
```

### `stories/{id}/original.json` & `translated.json`

```ts
type Chapter = {
  index: number
  title?: string                  // 章节标题（原文 / 译文）
  // 段落级切分，对照 immersive-translate 的输出形态
  // type 用来区分 AO3 的 .userstuff 内 p/blockquote/h2/hr 等
  blocks: Array<{
    id: string                    // 段落唯一 id，前端用作锚点
    type: "p" | "h2" | "blockquote" | "hr" | "pre"
    // 保留内嵌 HTML（em/strong/a 等），仅 sanitize 危险标签
    html: string
    // 仅 translated.json 用：翻译状态
    status?: "pending" | "done" | "error"
    error?: string
  }>
}

type ChapterFile = {
  chapters: Chapter[]
}
```

段落是翻译的最小单元。每段独立调 LLM，可断点续传、可重试单段。

### `stories/{id}/progress.json`

```ts
type Progress = {
  phase: "queued" | "fetching" | "parsing" | "translating" | "ready" | "error"
  totalBlocks: number
  doneBlocks: number
  currentChapter?: number
  startedAt: string
  finishedAt?: string
  errors: Array<{ chapterIndex: number; blockId: string; message: string; at: string }>
}
```

## API 设计

REST 风格，前端 `/api` 前缀，Vite dev 代理。

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/stories` | 入参 `{ url: string }`，返回 `{ id, status }`；后台开始抓取+翻译 |
| `GET` | `/api/stories` | 返回 `index.json` |
| `GET` | `/api/stories/:id` | meta + progress 概要 |
| `GET` | `/api/stories/:id/chapters/:n` | 单章节 original + translated 合并视图 |
| `GET` | `/api/stories/:id/stream` | SSE：推送 `progress` / `block-done` / `chapter-done` / `error` 事件 |
| `POST` | `/api/stories/:id/retry` | 入参 `{ blockIds?: string[]; chapterIndex?: number }`，重译失败段 |
| `DELETE` | `/api/stories/:id` | 删除（保留 source.html 备份选项） |
| `GET` | `/api/config` | 当前 provider 配置（apiKey 脱敏） |
| `PUT` | `/api/config` | 更新 provider 配置 |
| `POST` | `/api/config/test` | 用当前配置发一条 ping，验证连通 |

合并视图响应示例：

```ts
type ChapterView = {
  meta: Meta
  chapter: {
    index: number
    titleEn?: string
    titleZh?: string
    pairs: Array<{
      id: string
      type: string
      en: string                  // 原文 html
      zh?: string                 // 译文 html，可能为空（翻译中）
      status: "pending" | "done" | "error"
    }>
  }
  nav: { prev?: number; next?: number; total: number }
}
```

## 翻译流水线

```
POST /api/stories { url }
  └─→ 解析 work id
  └─→ enqueue(id, "fetch")

queue worker:
  1. fetch  : 从 AO3 拉 ?view_full_work=true&view_adult=true 的 HTML
              失败时重试 3 次（指数退避）
              保存到 source.html
  2. parse  : cheerio 解析 → meta.json + original.json
              段落 id 用 sha1(chapterIndex + html).slice(0,8)
              初始化 translated.json（blocks 全部 status=pending）
              更新 index.json status=translating
  3. translate:
       for each chapter:
         按 N 段为一批（默认 8 段，约 3-4k tokens）打包
         调 LLM：system prompt 要求保留内嵌标签、保留段落数、JSON 输出
         逐段写回 translated.json（fsync 防丢）
         SSE 推 block-done
       全部完成 → status=ready
       任一批失败 → 记录到 progress.errors，跳过继续，最后阶段标 error
```

### LLM Prompt 设计

System：

```
你是文学翻译。把英文文学作品翻译为中文，要求：
1) 严格保留输入中的 HTML 内联标签（em/strong/a/i/b 等），仅翻译文字内容
2) 每个输入段落必须对应一个输出段落，顺序、数量完全一致
3) 译文自然流畅，符合中文小说语感，不增不减
4) 角色名、地名等专有名词在故事内保持一致
5) 输入是一个 JSON 数组，输出也必须是同样长度的 JSON 数组
```

User payload：

```json
{
  "context": { "title": "...", "fandom": ["..."], "previous_glossary": {...} },
  "blocks": [
    { "id": "a3f9", "html": "He <em>smiled</em>." },
    { "id": "b1c2", "html": "..." }
  ]
}
```

输出强制 `response_format: { type: "json_object" }`（DeepSeek 和 OpenAI 都支持），结构 `{ "blocks": [{ "id": "a3f9", "html": "他<em>笑</em>了。" }] }`。

### 失败/恢复

- 单段失败：标记 `status: "error"` + 写错误信息，继续处理后续；前端在阅读器里渲染"重试此段"按钮
- 进程崩溃：启动时扫所有 `progress.json`，对 `phase` 不是 `ready`/`error` 的故事重新入队，从首个 `pending` 段开始续跑
- 整体重试：`POST /retry` 不带 body 等价于重置所有 error 段

## 阅读器迁移

现有 `build-reader-pages.mjs` 的 reader chrome（`#018eee` accent、Inter + Georgia/Songti、毛玻璃顶栏、滚动进度条、字号/中文比例/栏宽/明暗调节）整体搬到 React。

shadcn 组件映射：

| 现有元素 | shadcn 替换 |
|---|---|
| `.reader-topbar` | 自定义 `<header>` + Tailwind（不用 shadcn 包装） |
| 设置抽屉 | `Sheet` 或 `Popover` |
| 字号/比例 slider | `Slider` |
| 主题切换 | 自定义 ToggleGroup |
| 章节目录 | `Sheet` 左侧 + `Command` 搜索 |
| 书架卡片 | 沿用现有 `.work-row` 风格，用 Tailwind 还原 |

布局约定：

- `Library` 页：复刻 `list.html` 的 `x2146-fic.reader` 标题大字 + 列表
- `Reader` 页：左侧抽屉为章节目录，主区域单章节滚动；段落级双语对照沿用现状（英文 + 下方较小灰度中文）
- `Import` 页：单 input 贴链接 + 提交后跳到 `Reader` 显示进度
- `Settings` 页：LLM provider 配置 + 默认翻译选项

主题 token 直接搬：

```css
:root {
  --reader-accent: #018eee;
  --reader-font-size: 17px;
  --reader-zh-scale: 0.96;
  --reader-measure: 780px;
  /* light/dark surface 变量同上 */
}
```

阅读器本地状态（字号/比例/栏宽/主题）继续用 `localStorage`，key 沿用 `x2146.reader.*` 命名空间。

## 目录结构

```
x2146-fic/
├── DESIGN.md
├── package.json                  # workspaces: server, web, shared
├── server/
│   ├── package.json
│   ├── src/
│   │   ├── index.ts              # Hono 启动 + 静态文件托管 web/dist
│   │   ├── routes/
│   │   │   ├── stories.ts
│   │   │   ├── config.ts
│   │   │   └── stream.ts
│   │   ├── ao3/
│   │   │   ├── fetch.ts          # 处理 work id / download URL / cookies
│   │   │   └── parse.ts          # cheerio 提取 meta + chapters
│   │   ├── translate/
│   │   │   ├── provider.ts       # OpenAI 兼容 fetch
│   │   │   ├── prompt.ts
│   │   │   ├── chunker.ts        # 按段打包
│   │   │   └── worker.ts         # 队列消费
│   │   ├── db/
│   │   │   ├── index.ts          # lowdb 包装 + 文件分发
│   │   │   └── paths.ts
│   │   ├── queue.ts              # 内存队列 + 恢复
│   │   └── sse.ts                # 事件分发
│   └── data/                     # gitignore
├── web/
│   ├── package.json
│   ├── vite.config.ts            # proxy /api → :3000
│   ├── tailwind.config.ts
│   └── src/
│       ├── main.tsx
│       ├── routes/               # TanStack Router 文件式
│       │   ├── __root.tsx
│       │   ├── index.tsx         # Library
│       │   ├── import.tsx
│       │   ├── settings.tsx
│       │   └── reader.$id.$chapter.tsx
│       ├── components/
│       │   ├── ui/               # shadcn cli 安装位置
│       │   ├── reader/
│       │   │   ├── Topbar.tsx
│       │   │   ├── Pair.tsx      # 双语段落
│       │   │   ├── Settings.tsx
│       │   │   └── TOC.tsx
│       │   └── library/
│       ├── lib/
│       │   ├── api.ts            # fetch 封装
│       │   ├── sse.ts            # EventSource 工具
│       │   └── reader-state.ts   # zustand or 自定义
│       └── styles/
│           └── reader.css        # 沿用 reader chrome 的 token
└── shared/
    └── schema.ts                 # zod: Meta / Chapter / Index / Progress
```

## 部署

```
# server
bun install
bun run src/index.ts             # 默认 :3000

# web
bun install
bun run build                    # 产物 web/dist
```

`server/src/index.ts` 末尾：

```ts
app.use("/*", serveStatic({ root: "../web/dist" }))
app.fire?.()
Bun.serve({ port: 3000, fetch: app.fetch })
```

systemd unit 示例：

```ini
[Service]
WorkingDirectory=/opt/x2146-fic/server
ExecStart=/home/x/.bun/bin/bun run src/index.ts
Restart=on-failure
Environment=NODE_ENV=production
```

反代由 caddy/nginx 处理 TLS。AO3 出站请求需要的 cookie（成人内容确认）放在 `config.json` 里。

## 配置（`config.json`）

```ts
type Config = {
  llm: {
    baseURL: string               // https://api.deepseek.com/v1
    apiKey: string
    model: string                 // deepseek-chat
    temperature?: number          // 默认 0.3
    concurrency?: number          // 默认 3，单进程内并发请求数
    blocksPerRequest?: number     // 默认 8
  }
  ao3: {
    cookie?: string               // AO3 session cookie，用于成人内容
    userAgent?: string
  }
  reader: {
    defaultMeasure?: number       // 780
    defaultFont?: number          // 17
  }
}
```

不引入 `openai` SDK，`provider.ts` 直接 fetch：

```ts
await fetch(`${baseURL}/chat/completions`, {
  method: "POST",
  headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json" },
  body: JSON.stringify({
    model, temperature, response_format: { type: "json_object" },
    messages: [{ role: "system", content: systemPrompt }, { role: "user", content: userPayload }]
  })
})
```

## 迁移既有作品

当前已有 3 个 `*-zh-CN-dual.html` 文件，写一个一次性脚本 `server/src/scripts/import-legacy.ts`：

1. 读 dual HTML，cheerio 切出原文段 + `immersive-translate-target-wrapper` 译文段
2. 配对生成 `original.json` + `translated.json`，全部 `status: "done"`
3. 从 `<title>` 提 work id（如果有 URL 元信息），否则用 hash 当 id
4. 写入 `data/stories/{id}/`，加入 `index.json`

跑一次后 `list.html` 和 `*-reader.html` 都可以删除，新阅读器接管。

## 不做的事

- 不做用户系统 / 多租户：单用户工具
- 不做全文搜索：先用浏览器原生 `Ctrl+F`，规模真上来再加 fts5/minisearch
- 不做评论 / 笔记 / 高亮：以后再加
- 不做章节追更订阅 AO3：手动重新提交链接触发重译

## 风险与备注

- **AO3 反爬**：单用户低频访问通常没事；如被 429 走退避重试 + 写错误页
- **翻译 token 成本**：10 万字英文 ≈ 13 万 tokens，DeepSeek 输入 ¥0.27/M、输出 ¥1.1/M，整本 < ¥1
- **JSON 文件大小**：单本 10 万字 translated.json 约 1-2 MB，懒加载到章节就够了；如果单章节 >300 KB 再切分
- **HTML sanitize**：cheerio 解析时白名单内联标签（em/strong/a/i/b/sup/sub/br），AO3 自身已 sanitize 但稳妥起见再过一遍
- **`response_format: json_object` 的失败模式**：模型偶尔返回多/少段，要在 worker 里检测段数不匹配 → 标记此批失败重试一次 → 仍失败则按"输入段数 + 错位告警"标记 error
