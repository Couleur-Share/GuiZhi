# 从源码构建

## 平台支持边界

官方仅为 Windows 11+ x64 提供安装包、平台测试与维护支持。桌面外壳是 Go + WebView2，只面向 Windows；
Linux 和 macOS 没有桌面端构建入口，也不提供安装包、平台测试及兼容性保证。
平台专属问题不属于官方维护范围。欢迎提供复现步骤和验证结果的社区 PR，但不承诺修复或合并时限。

## 构建命令

需要 Windows 11 x64、Node.js ≥ 24、pnpm ≥ 11、Go（版本见 `apps/desktop-go/go.mod`）、PowerShell 7（`pwsh`）；
运行安装版需要系统带有 WebView2 运行时。

```bash
pnpm install

# 质量门禁
pnpm lint          # eslint + 文件行数门禁
pnpm typecheck     # TS 全量检查
pnpm test:unit     # 校验脚本 + 渲染层 vitest + jsrc 单测

# 构建与打包（Go 壳；没有 HMR 开发服务）
pnpm build              # 构建到 apps/desktop-go/out/win-x64
pnpm build:installer    # 在 build 产物上打 NSIS 安装包（apps/desktop-go/out/installer）
```

Go 侧在 `apps/desktop-go` 下运行 `go vet ./...` 与 `go test -count=1 ./...`（需先 `pnpm build`）。
安装包未做代码签名，首次安装可能触发 SmartScreen 提示。

界面改动想截图又不打扰本机：用 `pnpm shot`（隔离副本构建，窗口挪到屏幕外，数据目录为临时目录）。

## 仓库结构

```text
GuiZhi/
├── apps/desktop/
│   └── src/
│       ├── preload/     # window.api 白名单（经 desktop-go/shim 接到 Go）
│       └── renderer/    # React UI：library / ask / wiki / imports + 设置
├── apps/desktop-go/     # Go + WebView2 外壳：窗口/托盘、SQLite、IPC、采集、AI 代理、安装包与截图工具
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

渲染进程不直接碰系统能力，一切经 preload 白名单；远程抓取一律走 Go 壳的 SSRF 防护。

更细的架构约定、里程碑与编码规范见根目录 [AGENTS.md](../AGENTS.md)。Electron 时代关于数据库位置的评估见 [SQLite 与主进程](./sqlite-main-process.md)（历史资料）。
