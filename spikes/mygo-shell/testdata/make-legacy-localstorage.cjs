// 生成「真实 Electron 写出的 Local Storage」夹具，供 legacy_localstorage_test.go 读取。
// 用法：apps\desktop\node_modules\.bin\electron.cmd spikes\mygo-shell\testdata\make-legacy-localstorage.cjs <输出 userData 目录>
// 注意：用独立 userData，不碰真实 %APPDATA%\GuiZhi。页面用 file:// 加载，与正式版的 origin 一致。
const { app, BrowserWindow } = require("electron");
const fs = require("fs");
const os = require("os");
const path = require("path");

const out = process.argv[process.argv.length - 1];
if (!out || out.startsWith("-")) {
  console.error("缺少输出目录参数");
  process.exit(2);
}
app.setPath("userData", path.resolve(out));

app.whenReady().then(async () => {
  const html = path.join(os.tmpdir(), "guizhi-legacy-ls.html");
  fs.writeFileSync(html, "<!doctype html><meta charset=utf-8><title>ls</title>");
  const win = new BrowserWindow({ show: false });
  await win.loadFile(html);
  // 同时覆盖 Latin1 编码（纯 ASCII）与 UTF-16 编码（含中文、emoji、孤立代理项）的键和值。
  await win.webContents.executeJavaScript(`(() => {
    localStorage.clear();
    localStorage.setItem("guizhi-settings", JSON.stringify({ state: { language: "zh", theme: "dark", note: "归知" }, version: 3 }));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("视图-列配置", "标题,标签,来源");
    localStorage.setItem("emoji", "a😀b");
    localStorage.setItem("lone-surrogate", "x\\ud800y");
    localStorage.setItem("latin1-high", "caf\\u00e9");
    localStorage.setItem("empty", "");
    localStorage.setItem("big", "z".repeat(100000));
  })()`);
  // Chromium 把 localStorage 异步落盘（约 5 秒内），等一下再退出。
  await new Promise((r) => setTimeout(r, 7000));
  win.destroy();
  app.quit();
});
