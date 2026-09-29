// 分享入口 spike（仅 dev 包）：诊断记录的文件存取与设备信息。
// 用文件而不是 Preferences：扩展与主应用可能在不同进程，Preferences 不支持多进程。
// 写入采用“临时文件 + 重命名”，进程在写入中途被回收也不会留下半个文件。
import { common } from '@kit.AbilityKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { deviceInfo } from '@kit.BasicServicesKit';
import { appVersionLabel } from '../services/AppVersion';
import { ParsedEntries, ShareProbeEntry, formatReport, parseEntries, upsertEntries } from './ShareProbe';

const FILE_NAME: string = 'share-probe.jsonl';

// 机型与系统版本：写进报告，结论才有可追溯的环境。
export function deviceLine(): string {
  const model = deviceInfo.marketName ? deviceInfo.marketName : deviceInfo.productModel;
  return `${model} · ${deviceInfo.osFullName} · API ${deviceInfo.sdkApiVersion} · 归知 ${appVersionLabel()}`;
}

export class ShareDiagStore {
  private path: string;

  constructor(context: common.Context) { this.path = `${context.filesDir}/${FILE_NAME}`; }

  private read(): string {
    try { return fs.accessSync(this.path) ? fs.readTextSync(this.path) : ''; }
    catch { return ''; }
  }

  load(): ParsedEntries { return parseEntries(this.read()); }

  upsert(entry: ShareProbeEntry): void {
    const text = upsertEntries(this.read(), entry);
    const temp = `${this.path}.tmp`;
    const file = fs.openSync(temp, fs.OpenMode.CREATE | fs.OpenMode.WRITE_ONLY | fs.OpenMode.TRUNC);
    try { fs.writeSync(file.fd, text); }
    finally { fs.closeSync(file); }
    fs.renameSync(temp, this.path);
  }

  clear(): void {
    if (fs.accessSync(this.path)) fs.unlinkSync(this.path);
  }

  report(): string {
    const parsed = this.load();
    return formatReport(deviceLine(), parsed.entries, parsed.skipped, Date.now());
  }
}
