import type { CreateCrawlJobInput, WebScope } from "../types/web-capture";

export function canonicalWebUrl(raw: string): string {
  const url = new URL(raw);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.username ||
    url.password
  )
    throw new Error("请输入不含账号密码的 HTTP(S) 网址");
  url.hash = "";
  return url.href;
}
function safePath(path: string): string {
  if (/%2f|%5c/i.test(path)) throw new Error("目录包含编码路径分隔符");
  const decoded = decodeURIComponent(path);
  if (
    /\\|%2f|%5c|%2e/i.test(decoded) ||
    decoded.split("/").some((p) => p === ".." || p === ".")
  )
    throw new Error("目录包含含糊的编码或路径");
  return decoded;
}
export function webScope(entry: string, directory?: string): WebScope {
  const url = new URL(canonicalWebUrl(entry));
  const pathname =
    directory ?? url.pathname.slice(0, url.pathname.lastIndexOf("/") + 1);
  if (
    !pathname.startsWith("/") ||
    pathname.includes("?") ||
    pathname.includes("#")
  )
    throw new Error("目录必须为 / 开头的路径");
  const clean = safePath(pathname);
  return {
    origin: url.origin,
    directory: clean.endsWith("/") ? clean : `${clean}/`,
  };
}
export function inWebScope(raw: string, scope: WebScope): boolean {
  try {
    const url = new URL(canonicalWebUrl(raw));
    const path = safePath(url.pathname);
    return (
      url.origin === scope.origin &&
      (path.startsWith(scope.directory) ||
        path === scope.directory.slice(0, -1))
    );
  } catch {
    return false;
  }
}
export function isWebPageLink(raw: string): boolean {
  try {
    return !/\.(?:pdf|zip|gz|tar|exe|dmg|msi|mp[34]|png|jpe?g|gif|webp|svg|woff2?|css|js|xml)$/i.test(
      new URL(raw).pathname,
    );
  } catch {
    return false;
  }
}
export function validateCrawlInput(
  input: CreateCrawlJobInput,
): CreateCrawlJobInput {
  if (!input || !["documents", "research"].includes(input.purpose))
    throw new Error("采集用途无效");
  if (
    !Array.isArray(input.seeds) ||
    !input.seeds.length ||
    input.seeds.length > 10
  )
    throw new Error("请提供 1–10 个入口");
  const maxPages = input.maxPages ?? 50,
    maxDepth = input.maxDepth ?? 2;
  if (
    !Number.isInteger(maxPages) ||
    maxPages < 1 ||
    maxPages > 300 ||
    !Number.isInteger(maxDepth) ||
    maxDepth < 0 ||
    maxDepth > 5
  )
    throw new Error("页数须为 1–300，深度须为 0–5");
  return {
    purpose: input.purpose,
    maxPages,
    maxDepth,
    collectionId: input.collectionId ?? null,
    researchRunId: input.researchRunId,
    duplicatePolicy: input.duplicatePolicy === "update" ? "update" : "skip",
    seeds: input.seeds.map((seed) => {
      if (!["page", "directory"].includes(seed.mode))
        throw new Error("入口模式无效");
      const url = canonicalWebUrl(seed.url);
      const directory =
        seed.mode === "directory"
          ? webScope(url, seed.directory).directory
          : undefined;
      if (directory && !inWebScope(url, webScope(url, directory)))
        throw new Error("入口不在所选目录内");
      return { url, mode: seed.mode, directory };
    }),
  };
}
