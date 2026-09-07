import fs from "node:fs/promises";
import path from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { downloadToTempFile } from "../import/safe-fetch";
import { getImagesDir } from "../../runtime-paths";
import type { WebSnapshotAsset } from "@guizhi/shared/types";
import { cleanHtml } from "./snapshot-sanitize";

const leases = new Map<string, number>();
export const leasedSnapshotAssets = () => new Set(leases.keys());
export function releaseSnapshotAssets(assets: WebSnapshotAsset[]): void {
  for (const asset of assets) {
    const count = (leases.get(asset.fileName) ?? 1) - 1;
    if (count) leases.set(asset.fileName, count);
    else leases.delete(asset.fileName);
  }
}
function extension(data: Buffer): string | undefined {
  if (
    data.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))
  )
    return "png";
  if (data[0] === 255 && data[1] === 216 && data[2] === 255) return "jpg";
  if (/^GIF8[79]a/.test(data.subarray(0, 6).toString())) return "gif";
  if (
    data.subarray(0, 4).toString() === "RIFF" &&
    data.subarray(8, 12).toString() === "WEBP"
  )
    return "webp";
}
export async function collectSnapshotAssets(
  urls: string[],
  signal: AbortSignal,
) {
  const assets: WebSnapshotAsset[] = [],
    failures: { url: string; reason: string }[] = [];
  const unique = [...new Set(urls)],
    candidates = unique.slice(0, 200);
  failures.push(
    ...unique
      .slice(200)
      .map((url) => ({ url, reason: "每篇最多保存 200 个资源" })),
  );
  let index = 0,
    bytes = 0;
  await fs.mkdir(getImagesDir(), { recursive: true });
  const consume = async () => {
    while (index < candidates.length && !signal.aborted) {
      const url = candidates[index++];
      let temp: { dir: string; filePath: string } | undefined;
      try {
        if (bytes >= 100 * 1024 * 1024) throw new Error("文章资源超过 100 MiB");
        temp = await downloadToTempFile(url, {
          maxBytes: 20 * 1024 * 1024,
          fileName: "image",
          signal,
          referer: "https://mp.weixin.qq.com/",
        });
        let data = await fs.readFile(temp.filePath),
          ext = extension(data);
        if (!ext && /<svg[\s>]/i.test(data.subarray(0, 4096).toString())) {
          const { Resvg } = await import("@resvg/resvg-js");
          const svg = cleanHtml(data.toString("utf8"), () => undefined);
          const renderer = new Resvg(svg, {
            fitTo: { mode: "width", value: 1600 },
            font: { loadSystemFonts: false },
          });
          if (
            !Number.isFinite(renderer.height) ||
            renderer.height > 16000 ||
            renderer.width * renderer.height > 16000000
          )
            throw new Error("SVG 图片尺寸超过安全上限");
          data = Buffer.from(renderer.render().asPng());
          ext = "png";
        }
        if (!ext) throw new Error("资源不是支持的图片格式");
        bytes += data.length;
        if (data.length > 20 * 1024 * 1024 || bytes > 100 * 1024 * 1024)
          throw new Error("图片或文章资源超过大小上限");
        signal.throwIfAborted();
        const sha256 = createHash("sha256").update(data).digest("hex"),
          fileName = `wechat-${sha256}.${ext}`;
        // 同卷暂存后以硬链接原子发布，读者不会看到写到一半的共享资源。
        const staged = path.join(getImagesDir(), `.wechat-${randomUUID()}.tmp`);
        try {
          await fs.writeFile(staged, data, { flag: "wx" });
          await fs
            .link(staged, path.join(getImagesDir(), fileName))
            .catch((e) => {
              if (e.code !== "EEXIST") throw e;
            });
        } finally {
          await fs.rm(staged, { force: true });
        }
        leases.set(fileName, (leases.get(fileName) ?? 0) + 1);
        assets.push({ fileName, sha256, sourceUrl: url, bytes: data.length });
      } catch (error) {
        failures.push({
          url,
          reason: error instanceof Error ? error.message : "图片保存失败",
        });
      } finally {
        if (temp) await fs.rm(temp.dir, { recursive: true, force: true });
      }
    }
  };
  await Promise.all(Array.from({ length: 4 }, consume));
  failures.push(
    ...candidates
      .slice(index)
      .map((url) => ({ url, reason: "采集已取消或超时" })),
  );
  return { assets, failures };
}
