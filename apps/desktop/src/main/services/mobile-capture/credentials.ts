import { safeStorage } from "electron";
import fs from "node:fs";
import path from "node:path";
import { getUserDataPath } from "../../runtime-paths";
export interface CaptureConnection {
  origin: string; mailboxId: string; credential: string; requestId: string;
  paused: boolean; collectionId: string | null; notice?: string;
}
export class CaptureCredentials {
  private file = path.join(getUserDataPath(), ".machine", "mobile-capture.json");
  secure() {
    try { return safeStorage.isEncryptionAvailable() && (process.platform !== "linux" || safeStorage.getSelectedStorageBackend?.() !== "basic_text"); }
    catch { return false; }
  }
  clear() { if (fs.existsSync(this.file)) fs.unlinkSync(this.file); }
  read(): CaptureConnection | null {
    if (!fs.existsSync(this.file)) return null;
    if (!this.secure()) throw new Error("安全存储不可用，请在本次运行重新配对");
    try {
      const saved = JSON.parse(fs.readFileSync(this.file, "utf8"));
      const value = JSON.parse(safeStorage.decryptString(Buffer.from(saved.encrypted, "base64"))) as CaptureConnection;
      if (typeof value.credential !== "string" || typeof value.origin !== "string") throw new Error();
      return value;
    } catch { throw new Error("无法解锁手机收集凭证，请新建收件箱并重新配对"); }
  }
  write(connection: CaptureConnection) {
    if (!this.secure()) return;
    fs.mkdirSync(path.dirname(this.file), { recursive: true, mode: 0o700 });
    const temp = `${this.file}.tmp`;
    fs.writeFileSync(temp, JSON.stringify({ encrypted: safeStorage.encryptString(JSON.stringify(connection)).toString("base64") }), { mode: 0o600 });
    fs.renameSync(temp, this.file);
  }
}
