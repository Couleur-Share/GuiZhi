// goja 里的 node:path 极简实现（POSIX 语义）：vfile 只用到 basename / dirname / extname / join / sep。
const sep = "/";
function normalizeParts(path: string): string {
  const abs = path.startsWith("/");
  const out: string[] = [];
  for (const part of path.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") { if (out.length && out[out.length - 1] !== "..") out.pop(); else if (!abs) out.push(".."); } else out.push(part);
  }
  return (abs ? "/" : "") + out.join("/") || (abs ? "/" : ".");
}
export function join(...parts: string[]): string { const joined = parts.filter(Boolean).join("/"); return joined ? normalizeParts(joined) : "."; }
export function basename(path: string, ext?: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const base = trimmed.slice(trimmed.lastIndexOf("/") + 1);
  return ext && base.endsWith(ext) && base !== ext ? base.slice(0, -ext.length) : base;
}
export function dirname(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const i = trimmed.lastIndexOf("/");
  return i < 0 ? "." : i === 0 ? "/" : trimmed.slice(0, i);
}
export function extname(path: string): string {
  const base = basename(path), i = base.lastIndexOf(".");
  return i <= 0 ? "" : base.slice(i);
}
export default { sep, join, basename, dirname, extname };
export { sep };