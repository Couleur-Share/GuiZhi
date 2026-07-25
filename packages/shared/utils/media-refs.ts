/**
 * 媒体资产引用解析：从条目内容中提取 local-image:// / local-video://
 * 协议的资产文件名（媒体条目的内容首行由导入连接器生成该引用）。
 */

export type LocalAssetProtocol = "local-image" | "local-video";

const ASSET_NAME_PATTERN = "([A-Za-z0-9_.-]+)";

export function extractLocalAssetRef(
  content: string,
  protocol: LocalAssetProtocol,
): string | null {
  const match = content.match(new RegExp(`${protocol}://${ASSET_NAME_PATTERN}`));
  return match?.[1] ?? null;
}

/**
 * 按出现顺序提取某一协议的全部资产引用。
 * 图文条目会引用多张图片，逐张 OCR 时需要拿全。
 */
export function extractLocalAssetRefs(
  content: string,
  protocol: LocalAssetProtocol,
): string[] {
  if (!content) {
    return [];
  }
  const pattern = new RegExp(`${protocol}://${ASSET_NAME_PATTERN}`, "g");
  const refs: string[] = [];
  for (const match of content.matchAll(pattern)) {
    if (isSafeAssetFileName(match[1]) && !refs.includes(match[1])) {
      refs.push(match[1]);
    }
  }
  return refs;
}

/**
 * 提取全部资产引用（一条笔记可以引用多个资产）。
 *
 * 删除条目后要据此清理磁盘文件，所以这里必须取全，
 * 只看首个匹配会漏掉同一条目里的其余附件。
 */
export function extractAllLocalAssetRefs(content: string): string[] {
  if (!content) {
    return [];
  }
  const refs = new Set<string>();
  for (const protocol of ["local-image", "local-video"] as const) {
    const pattern = new RegExp(`${protocol}://${ASSET_NAME_PATTERN}`, "g");
    for (const match of content.matchAll(pattern)) {
      if (isSafeAssetFileName(match[1])) {
        refs.add(match[1]);
      }
    }
  }
  return [...refs];
}

/**
 * 资产文件名是否可以安全地拼进资产目录。
 *
 * 引用来自条目正文，而正文是用户可编辑的：`local-image://..` 拼出来就是
 * 资产目录的父目录。删除路径上尤其不能信任它。
 */
export function isSafeAssetFileName(fileName: string): boolean {
  return (
    /^[A-Za-z0-9_.-]+$/.test(fileName) &&
    fileName !== "." &&
    fileName !== ".." &&
    !fileName.startsWith(".")
  );
}
