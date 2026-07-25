import { isWebRuntime } from "../runtime";

function isExternalMediaSrc(src: string): boolean {
  return /^(https?:|data:|blob:)/i.test(src);
}

function isResolvedWebMediaSrc(
  src: string,
  kind: "images" | "videos",
): boolean {
  return src.startsWith(`/api/media/${kind}/`);
}

function decodeLocalMediaFileName(
  src: string,
  protocol: "local-image" | "local-video",
): string {
  const prefix = `${protocol}://`;
  if (!src.startsWith(prefix)) {
    return src;
  }

  const encodedFileName = src.slice(prefix.length);
  try {
    return decodeURIComponent(encodedFileName);
  } catch {
    return encodedFileName;
  }
}

export function resolveLocalImageSrc(src: string): string {
  if (!src || isExternalMediaSrc(src) || isResolvedWebMediaSrc(src, "images")) {
    return src;
  }

  const fileName = decodeLocalMediaFileName(src, "local-image");
  if (isWebRuntime()) {
    return `/api/media/images/${encodeURIComponent(fileName)}`;
  }
  return `local-image://${encodeURIComponent(fileName)}`;
}

export function resolveLocalGenerationImageSrc(src: string): string {
  if (!src || isExternalMediaSrc(src)) return src;
  const prefix = "local-generation-image://";
  const encodedPath = src.startsWith(prefix) ? src.slice(prefix.length) : src;
  return `${prefix}${encodeURIComponent(decodeURIComponent(encodedPath))}`;
}

export function resolveLocalVideoSrc(src: string): string {
  if (!src || isExternalMediaSrc(src) || isResolvedWebMediaSrc(src, "videos")) {
    return src;
  }

  const fileName = decodeLocalMediaFileName(src, "local-video");
  if (isWebRuntime()) {
    return `/api/media/videos/${encodeURIComponent(fileName)}`;
  }
  return `local-video://${encodeURIComponent(fileName)}`;
}
