import { restoreLocalImageUrl } from "./media-urls";

/**
 * 网页快照里的本地图片：由阅读页（可信的主框架）读出来，内联成 data: 地址再交给沙箱 iframe。
 *
 * 为什么不让 iframe 自己去取 `./media/local-image/…`：iframe 是 `sandbox="allow-scripts"`、没有 allow-same-origin，来源不透明。
 * 在真 WebView2 里实测（readersmoke 的 snapshot 场景，台账 R-15）：不透明来源的请求到不了岛的自有地址
 * （https://gzreader.viva-app.invalid/…，宿主的媒体回调一次都没被调用），同样的页面换成 `allow-same-origin` 就能取到——
 * 与内容安全策略无关。而 allow-same-origin 加 allow-scripts 会让 iframe 里的脚本摘掉自己的沙箱，是台账 A-018 的红线，不能开。
 * blob: 地址也试过：主框架创建的 blob 在不透明来源的 iframe 里加载不出来；data: 地址可以。
 *
 * 所以只由主框架读图：宿主已把文档里的 `local-image://<名称>` 改写成 `./media/local-image/<名称>`（viva/reader 的 AdaptSnapshotDocument），
 * 这里按同一个写法找出引用、经岛自己的媒体路径（同源 fetch，宿主校验名称与扩展名）取回字节，换成 data: 地址。
 * 不放开任何外部来源：岛的策略 img-src 本来就含 data:，文档自带的 <meta> 策略里的图片来源也由宿主改成只许 data:。
 */

/** 文档里一处岛内本地图片地址：名称到引号、括号、空白、尖括号、& 与反斜杠为止（与 viva/reader 的 localImageRef 同一个范围）。 */
const LOCAL_IMAGE_REF = /\.\/media\/local-image\/[^"'()\s<>&\\]+/g;

/** 一份文档内联的图片字节总量上限：超出的图片保持原样（页面里显示成裂图），不让一篇文章把页面内存撑爆。 */
export const MAX_INLINE_IMAGE_BYTES = 16 << 20;
/** 同时在途的读图请求数。 */
const CONCURRENCY = 6;

/** 文档里引用的本地图片地址（去重，保持出现顺序）。 */
export function localImageRefs(html: string): string[] {
  return [...new Set(html.match(LOCAL_IMAGE_REF) ?? [])];
}

export interface InlinedSnapshot {
  /** 本地图片换成 data: 地址之后的文档；取不到或超出预算的图片保持原来的相对地址。 */
  doc: string;
  /**
   * data: 地址 → 图片清单里的原始写法（`local-image://<名称>`）：桥接脚本点图时回报的是 data: 地址，要靠它找回序号。
   * 每个引用的 data: 地址末尾带一个 `#<引用序号>` 片段（不影响图片解码）：两张字节相同的图也各有各的地址，点哪张就是哪张。
   */
  originals: Map<string, string>;
}

interface Options {
  /** 读图用的 fetch；缺省是页面的 fetch。 */
  fetch?: typeof fetch;
  signal?: AbortSignal;
  maxBytes?: number;
}

function readAsDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.readAsDataURL(blob);
  });
}

/**
 * 读出 refs 里的每张图并换进文档。单张图失败（网络、状态码、不是图片、超出预算）只让这一张保持原样，不影响其余。
 * 被中止（signal）时抛出 AbortError，调用方丢弃结果。
 */
export async function inlineLocalImages(
  html: string,
  refs: string[],
  options: Options = {},
): Promise<InlinedSnapshot> {
  const fetchImage = options.fetch ?? ((input: string, init?: RequestInit) => fetch(input, init));
  const maxBytes = options.maxBytes ?? MAX_INLINE_IMAGE_BYTES;
  const dataUrls = new Map<string, string>(); // 引用 → 带片段的 data: 地址
  let used = 0;
  let next = 0;
  const worker = async (): Promise<void> => {
    while (next < refs.length) {
      const index = next++;
      const ref = refs[index];
      if (options.signal?.aborted) return;
      try {
        const response = await fetchImage(ref, { signal: options.signal });
        if (!response.ok) continue;
        const blob = await response.blob();
        if (!blob.type.startsWith("image/") || used + blob.size > maxBytes) continue;
        used += blob.size;
        dataUrls.set(ref, `${await readAsDataUrl(blob)}#${index}`);
      } catch {
        /* 这一张保持原样 */
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(CONCURRENCY, refs.length) }, worker));
  if (options.signal?.aborted) throw new DOMException("aborted", "AbortError");

  const originals = new Map<string, string>();
  for (const [ref, dataUrl] of dataUrls) originals.set(dataUrl, restoreLocalImageUrl(ref));
  const doc = dataUrls.size === 0 ? html : html.replace(LOCAL_IMAGE_REF, (ref) => dataUrls.get(ref) ?? ref);
  return { doc, originals };
}
