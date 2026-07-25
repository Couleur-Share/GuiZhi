/**
 * 媒体资产引用解析：从条目内容中提取 local-image:// / local-video://
 * 协议的资产文件名（媒体条目的内容首行由导入连接器生成该引用）。
 */

export type LocalAssetProtocol = "local-image" | "local-video";

export function extractLocalAssetRef(
  content: string,
  protocol: LocalAssetProtocol,
): string | null {
  const match = content.match(
    new RegExp(`${protocol}://([A-Za-z0-9_.-]+)`),
  );
  return match?.[1] ?? null;
}
