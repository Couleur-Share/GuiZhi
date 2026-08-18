const fs = require("node:fs");
const path = require("node:path");

// 与 src/utils/changelog.ts 的 VERSION_HEADING_SOURCE 保持一致
const VERSION_HEADING = /^## \[?v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?\]?/gm;

/**
 * 只取 CHANGELOG 最新一节写进更新清单。整份塞进去会让 latest.yml
 * 随版本无限增长，用户每次检查更新都要下载全部历史。
 */
function readLatestChangelogSection() {
  const content = fs.readFileSync(
    path.join(__dirname, "../../CHANGELOG.md"),
    "utf8",
  );
  const headings = [...content.matchAll(VERSION_HEADING)];

  if (headings.length === 0) {
    return "";
  }

  return content
    .slice(headings[0].index, headings[1]?.index ?? content.length)
    .replace(/^---\s*$/gm, "")
    .trim();
}

const extraResources = [
  {
    from: "resources/icon.ico",
    to: "icon.ico",
  },
  {
    from: "resources/icon.png",
    to: "icon.png",
  },
  // Dock 用 mac.icon → icon.png（归知）。icon.iconset 仍是 PromptHub
  // 图案，仅作托盘模板缺失时的回退；有证书前不阻塞发布。
  {
    from: "resources/icon.iconset",
    to: "icon.iconset",
  },
  {
    from: "resources/tray",
    to: "tray",
  },
  {
    from: "../../CHANGELOG.md",
    to: "CHANGELOG.md",
  },
  // MCP server：放在 asar 外面，用户要在 mcp.json 里按绝对路径引用它
  {
    from: "out/mcp",
    to: "mcp",
  },
  // MCP server 是独立进程，必须用与应用同一份 wasm 驱动——它的锁是
  // `mkdir <db>.lock` 目录，跟原生 SQLite 的字节范围锁互不认识，换驱动会
  // 读到未提交的数据。放在产物旁的 node_modules 下靠 Node 常规解析找到，
  // 比手拼 app.asar.unpacked 路径可靠。
  {
    from: "../../node_modules/node-sqlite3-wasm",
    to: "mcp/node_modules/node-sqlite3-wasm",
  },
];

/**
 * 源不存在时 electron-builder 只是跳过那一条，不报错也不中止。MCP server
 * 产物就这么在 v0.11~v0.13 三个安装包里整个缺席（CI 当时跑的是 vite build，
 * 不含 build:mcp），用户侧唯一的迹象是设置页一句「组件缺失」。
 * 打包前逐条确认，缺了当场失败。
 */
function assertExtraResourcesExist() {
  const missing = extraResources
    .map((entry) => entry.from)
    .filter((from) => !fs.existsSync(path.resolve(__dirname, from)));

  if (missing.length > 0) {
    throw new Error(
      `extraResources 源缺失：${missing.join("、")}。` +
        `out/ 下的产物需要先执行 pnpm build（含 build:mcp）。`,
    );
  }
}

const enableMacReleaseSigning =
  process.env.GUIZHI_MAC_RELEASE_SIGN === "true";

// 未签名公开分发：identity "-" 走 electron-builder 官方 ad-hoc（需 ≥26）。
// hardenedRuntime 保持 false——26.0.13+ 若对 ad-hoc 开 hardenedRuntime，
// 未配 entitlements 时易撞 Team ID / 媒体权限问题；有证书的正式路径才开。
const macReleaseSigningConfig = enableMacReleaseSigning
  ? {
      hardenedRuntime: true,
      notarize: true,
      entitlements: "resources/entitlements.mac.plist",
      entitlementsInherit: "resources/entitlements.mac.inherit.plist",
    }
  : {
      hardenedRuntime: false,
      identity: "-",
      notarize: false,
    };

module.exports = {
  appId: "com.couleurshare.guizhi",
  productName: "GuiZhi",
  directories: {
    output: "dist",
    buildResources: "resources",
  },
  extraMetadata: {
    main: "out/main/index.js",
  },
  files: [
    "out/**/*",
    // 主进程将 playwright-core 保持为 external，避免 Rollup 提升其内部可选
    // require；electron-builder 默认依赖收集会忽略 Playwright，因此显式只带
    // 控制库。系统浏览器由 BrowserCaptureService 选择，不携带下载的浏览器。
    {
      from: "node_modules/playwright-core",
      to: "node_modules/playwright-core",
      filter: ["**/*", "!**/.local-browsers/**"],
    },
  ],
  extraResources,
  beforePack: () => {
    assertExtraResourcesExist();
  },
  asarUnpack: ["**/*.node", "**/node-sqlite3-wasm/**"],
  asar: true,
  npmRebuild: false,
  nodeGypRebuild: false,
  mac: {
    target: [
      {
        target: "dmg",
        arch: ["x64", "arm64"],
      },
      {
        target: "zip",
        arch: ["x64", "arm64"],
      },
    ],
    artifactName: "${productName}-${version}-${arch}.${ext}",
    // 归知 PNG ≥512（electron-builder 26 硬门槛）；icon.png 仍是 256 供其它用途。
    // 旧 icon.icns / icon.iconset 仍是 PromptHub 图案。
    icon: "resources/icon-mac.png",
    category: "public.app-category.productivity",
    gatekeeperAssess: false,
    ...macReleaseSigningConfig,
  },
  win: {
    target: [
      {
        target: "nsis",
        arch: ["x64", "arm64"],
      },
    ],
    artifactName: "${productName}-Setup-${version}-${arch}.${ext}",
    icon: "resources/icon.ico",
  },
  linux: {
    // scoped 包名 @guizhi/desktop 会被收成 @guizhidesktop，AppImage 拒收 @
    executableName: "GuiZhi",
    target: [
      {
        target: "AppImage",
        arch: ["x64"],
      },
      {
        target: "deb",
        arch: ["x64"],
      },
    ],
    category: "Utility",
    artifactName: "${productName}-${version}-${arch}.${ext}",
  },
  appImage: {
    artifactName: "${productName}-${version}-x64.${ext}",
  },
  nsis: {
    guid: "fa5aa350-68bd-43b9-b440-a7ecb8fcc78a",
    oneClick: false,
    allowToChangeInstallationDirectory: true,
    createDesktopShortcut: true,
    createStartMenuShortcut: true,
    differentialPackage: false,
    include: "resources/installer.nsh",
  },
  publish: {
    provider: "github",
    owner: "Couleur-Share",
    repo: "GuiZhi",
    releaseType: "release",
  },
  releaseInfo: {
    releaseNotes: readLatestChangelogSection(),
  },
};
