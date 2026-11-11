// 打进 goja 的入口：直接引用 TS 原文件，保证净化行为与 Electron 版逐字节一致（升级 sanitize-html/css-tree 后重新打包即可）。
import { cleanHtml, cleanCss, sanitizeSnapshot, snapshotHash } from "../../apps/desktop/src/main/services/web-capture/snapshot-sanitize";
import { snapshotDocument, escapeSnapshotText } from "../../apps/desktop/src/main/services/web-capture/snapshot-document";
import { SNAPSHOT_BRIDGE } from "../../apps/desktop/src/main/services/web-capture/snapshot-bridge";

/** 资源映射以普通对象传入（原始 URL → 本快照资源 URL），避免跨 JS/Go 边界传函数。 */
const mapper = (table: Record<string, string>) => (url: string) => Object.prototype.hasOwnProperty.call(table, url) ? table[url] : undefined;

(globalThis as Record<string, unknown>).GZ = {
  cleanHtml: (html: string, table: Record<string, string>) => cleanHtml(html, mapper(table)),
  cleanCss: (css: string, table: Record<string, string>, stylesheet: boolean) => cleanCss(css, mapper(table), stylesheet),
  sanitizeSnapshot: (json: string) => JSON.stringify(sanitizeSnapshot(JSON.parse(json))),
  snapshotHash: (json: string) => snapshotHash(JSON.parse(json)),
  snapshotDocument: (json: string, instanceId?: string) => snapshotDocument(JSON.parse(json), instanceId),
  escapeSnapshotText,
  bridge: SNAPSHOT_BRIDGE,
};
