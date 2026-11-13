import { SNAPSHOT_BRIDGE } from "./snapshot-bridge";
import { createHash } from "node:crypto";
import type { WebSnapshot } from "@guizhi/shared/types";
import { sanitizeSnapshot } from "./snapshot-sanitize";

const escape = (s: string) =>
  s.replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
export function snapshotDocument(
  snapshot: WebSnapshot,
  instanceId?: string,
): string {
  const safe = sanitizeSnapshot(snapshot);
  // 唯一允许的脚本由应用生成，不拼入任何网页脚本或属性。
  const bridge = instanceId ? SNAPSHOT_BRIDGE : "";
  const hash = createHash("sha256").update(bridge).digest("base64");
  const csp = `default-src 'none'; script-src ${bridge ? `'sha256-${hash}'` : "'none'"}; style-src 'unsafe-inline'; img-src ${instanceId ? "local-image:" : "'self'"}; connect-src 'none'; font-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'`;
  return `<!doctype html><html data-instance="${escape(instanceId ?? "")}"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${escape(csp)}"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html{margin:0;padding:0;background:white;color:#222}body{margin:0;padding:16px;box-sizing:border-box;font:16px/1.75 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;overflow-wrap:anywhere}img{max-width:100%;height:auto}*{box-sizing:border-box}pre,table{max-width:100%;overflow:auto}${safe.css}</style></head><body>${safe.html}${bridge ? `<script>${bridge}</script>` : ""}</body></html>`;
}
export { escape as escapeSnapshotText };
