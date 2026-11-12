// MyGo 壳（Go + WebView2）的安装包配置：直接复用现有 Electron 版的 NSIS 模板与发布设置，
// 用 electron-builder 的 --prepackaged 把 build-shell.ps1 产出的目录打成安装包，并生成同格式的 latest.yml。
//   - appId、productName、固定 guid、安装目录可选、快捷方式、publish 都来自 apps/desktop/electron-builder.config.cjs；
//     同一 guid 让它作为同一产品的「升级」覆盖安装 Electron 版（先由模板运行旧版卸载程序再装新文件）。
//   - 安装器多出两处检查（WebView2 运行时、覆盖安装前的备份提示），在 installer-mygo-checks.nsh，
//     只在这个配置下通过 GUIZHI_MYGO_SHELL 打开，Electron 版打包不受影响。
//   - 不签名（与现有 Windows 安装包一致）；卸载不删除 %APPDATA%\GuiZhi 用户数据（electron-builder 默认如此）。
const fs = require("node:fs");
const path = require("node:path");
const base = require("../../apps/desktop/electron-builder.config.cjs");

const root = path.resolve(__dirname, "../..");
const out = path.join(__dirname, "out", "installer");
fs.mkdirSync(out, { recursive: true });

// 包装文件：先打开开关、载入检查宏，再包含原模板（NSIS 的 include 路径以绝对路径写入，不依赖构建时的工作目录）
const nsisPath = (p) => p.replace(/\//g, "\\");
const wrapper = path.join(out, "installer-include.nsh");
fs.writeFileSync(
  wrapper,
  [
    "!define GUIZHI_MYGO_SHELL",
    `!include "${nsisPath(path.join(__dirname, "installer-mygo-checks.nsh"))}"`,
    `!include "${nsisPath(path.join(root, "apps/desktop/resources/installer.nsh"))}"`,
    "",
  ].join("\r\n"),
);

module.exports = {
  appId: base.appId,
  productName: base.productName,
  directories: { output: out, buildResources: path.join(root, "apps/desktop/resources") },
  win: {
    target: [{ target: "nsis", arch: ["x64"] }],
    artifactName: base.win.artifactName,
    icon: path.join(root, "apps/desktop/resources/icon.ico"),
  },
  nsis: { ...base.nsis, include: wrapper },
  publish: base.publish,
  releaseInfo: base.releaseInfo,
};
