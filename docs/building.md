# 从源码构建

需要 Node.js ≥ 24、pnpm ≥ 11。

```bash
pnpm install

# 开发（Vite HMR + Electron）
pnpm electron:dev

# 质量门禁
pnpm lint          # eslint + 文件行数门禁
pnpm typecheck     # TS 全量检查
pnpm test:unit     # vitest 单测
pnpm test:e2e      # Playwright 真实 Electron 冒烟

# 打包（须用 pnpm build，含 MCP 产物；不要只跑 vite build）
pnpm electron:build:win
pnpm electron:build:mac    # 须在 macOS 上；默认 ad-hoc 签名
pnpm electron:build:linux
```

macOS 正式 Developer ID 签名与公证需设 `GUIZHI_MAC_RELEASE_SIGN=true` 并配置
证书相关环境变量；未配置时 `mac.identity` 为 `"-"`（electron-builder ≥26 官方
ad-hoc），用户需手动绕过 Gatekeeper。

界面改动想截图又不打扰本机：用 `pnpm shot`（窗口挪到屏幕外，数据目录为临时目录）。源码比 `out/` 新时会提示先 build，确认无所谓可用 `--stale-ok`。

## 仓库结构

```text
GuiZhi/
├── apps/desktop/
│   └── src/
│       ├── main/        # Electron 主进程：窗口/托盘、SQLite、IPC、采集与媒体
│       ├── preload/     # contextBridge 白名单
│       ├── renderer/    # React UI：library / ask / wiki / imports + 设置
│       └── mcp/         # 独立 MCP server（随应用打包）
├── packages/
│   ├── core/            # AI 配置与客户端、运行时路径
│   ├── db/              # SQLite 适配器、schema、迁移
│   └── shared/          # 跨进程类型、IPC 频道、平台检测与协议工具
├── docs/                # 本目录：用户与开发者文档
├── .github/workflows/   # 质量检查 + 打 tag 构建发布
├── AGENTS.md            # 开发者与 AI 助手指南
├── CHANGELOG.md
└── NOTICE               # PromptHub 归属声明
```

渲染进程不直接碰 Node API，一切系统能力经 preload 白名单；远程抓取一律走主进程的 SSRF 防护。

更细的架构约定、里程碑与编码规范见根目录 [AGENTS.md](../AGENTS.md)。数据库为何留在主进程见 [SQLite 与主进程](./sqlite-main-process.md)。
