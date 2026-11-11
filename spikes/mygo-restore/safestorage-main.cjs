// 在隔离的 userData 里用 Electron 真实的 safeStorage 加密/解密，用来验证 Go 能否互通。
// 用法：electron safestorage-main.cjs <userDataDir> encrypt <明文> <输出文件>
//       electron safestorage-main.cjs <userDataDir> decrypt <输入文件> <输出文件>
const { app, safeStorage } = require("electron");
const fs = require("node:fs");
const [dir, mode, a, b] = process.argv.slice(process.argv.indexOf("--") + 1 > 0 && process.argv.includes("--") ? process.argv.indexOf("--") + 1 : 2);
app.setPath("userData", dir);
app.disableHardwareAcceleration();
app.whenReady().then(() => {
  const info = { available: safeStorage.isEncryptionAvailable() };
  try { info.backend = safeStorage.getSelectedStorageBackend(); } catch (e) { info.backend = "unavailable:" + e.message; }
  if (mode === "encrypt") fs.writeFileSync(b, safeStorage.encryptString(a).toString("base64"));
  else fs.writeFileSync(b, safeStorage.decryptString(Buffer.from(fs.readFileSync(a, "utf8"), "base64")));
  fs.writeFileSync(b + ".info.json", JSON.stringify(info));
  app.quit();
}).catch((e) => { console.error(e); app.exit(2); });
