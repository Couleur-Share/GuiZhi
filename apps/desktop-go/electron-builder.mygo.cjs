// MyGo 壳（Go + WebView2）的安装包配置：用 electron-builder 的 --prepackaged 把 build-shell.ps1 产出的目录打成 NSIS 安装包，
// 并生成 latest.yml。本文件自包含，不再依赖任何 Electron 应用的配置。
//   - appId、productName、固定 guid 沿用历史值：同一 guid 让新版本作为同一产品的「升级」覆盖安装旧版本
//     （先由模板运行旧版卸载程序再装新文件）。这些值一旦改动，老用户就无法原地升级，不要改。
//   - 安装器多出两处检查（WebView2 运行时、覆盖安装前的备份提示），在 installer/installer-mygo-checks.nsh，
//     通过 GUIZHI_MYGO_SHELL 打开。
//   - 不签名；卸载不删除 %APPDATA%\GuiZhi 用户数据（electron-builder 默认如此）。
const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "../..");
const out = path.join(__dirname, "out", "installer");
const installerDir = path.join(__dirname, "installer");
fs.mkdirSync(out, { recursive: true });

// 与 apps/desktop/src/utils/changelog.ts 的 VERSION_HEADING_SOURCE 保持一致
const VERSION_HEADING = /^## \[?v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?\]?/gm;

/**
 * 只取 CHANGELOG 最新一节写进更新清单。整份塞进去会让 latest.yml
 * 随版本无限增长，用户每次检查更新都要下载全部历史。
 */
function readLatestChangelogSection() {
  const content = fs.readFileSync(path.join(root, "CHANGELOG.md"), "utf8");
  const headings = [...content.matchAll(VERSION_HEADING)];
  if (headings.length === 0) return "";
  return content
    .slice(headings[0].index, headings[1]?.index ?? content.length)
    .replace(/^---\s*$/gm, "")
    .trim();
}

// 包装文件：先打开开关、载入检查宏，再包含原模板（NSIS 的 include 路径以绝对路径写入，不依赖构建时的工作目录）
const nsisPath = (p) => p.replace(/\//g, "\\");
const wrapper = path.join(out, "installer-include.nsh");
fs.writeFileSync(
  wrapper,
  [
    "!define GUIZHI_MYGO_SHELL",
    `!include "${nsisPath(path.join(installerDir, "installer-mygo-checks.nsh"))}"`,
    `!include "${nsisPath(path.join(installerDir, "installer.nsh"))}"`,
    "",
  ].join("\r\n"),
);

module.exports = {
  appId: "com.couleurshare.guizhi",
  productName: "GuiZhi",
  // electron-builder 即使走 --prepackaged 也要求能算出 Electron 版本，否则拒绝运行。
  // 这里只是满足该检查的占位值：Go 壳不含 Electron，构建也不会下载它。
  electronVersion: "45.2.3",
  // 版本号与 build-shell.ps1 同源（apps/desktop/package.json，发版时改的就是它）。
  // 本包自己的 package.json 版本是 0.0.0，不指定会让安装包文件名和 latest.yml 都变成 0.0.0。
  extraMetadata: { version: require("../desktop/package.json").version },
  directories: { output: out, buildResources: installerDir },
  win: {
    target: [{ target: "nsis", arch: ["x64"] }],
    artifactName: "${productName}-Setup-${version}-${arch}.${ext}",
    icon: path.join(installerDir, "icon.ico"),
  },
  nsis: {
    guid: "fa5aa350-68bd-43b9-b440-a7ecb8fcc78a",
    oneClick: false,
    allowToChangeInstallationDirectory: true,
    createDesktopShortcut: true,
    createStartMenuShortcut: true,
    differentialPackage: false,
    include: wrapper,
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
