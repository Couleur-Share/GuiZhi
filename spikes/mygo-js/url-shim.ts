// goja 里的 node:url 极简实现：vfile 只在传入 URL 对象时用 fileURLToPath。
export function fileURLToPath(url: string | { pathname: string; protocol: string }): string {
  const value = typeof url === "string" ? new URL(url) : url;
  if (value.protocol !== "file:") throw new TypeError("The URL must be of scheme file");
  return decodeURIComponent(value.pathname);
}
export default { fileURLToPath };