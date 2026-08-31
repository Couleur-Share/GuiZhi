import fs from "node:fs";
import { Zip, ZipPassThrough } from "fflate";

export type PortableSource =
  | { archivePath: string; filePath: string }
  | { archivePath: string; data: Buffer };

function addBuffer(zip: Zip, archivePath: string, data: Buffer): void {
  const entry = new ZipPassThrough(archivePath);
  zip.add(entry);
  entry.push(data, true);
}

function addFile(zip: Zip, archivePath: string, filePath: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const entry = new ZipPassThrough(archivePath);
    zip.add(entry);
    const stream = fs.createReadStream(filePath, { highWaterMark: 1024 * 1024 });
    stream.on("data", (chunk) => entry.push(Buffer.from(chunk), false));
    stream.on("end", () => {
      entry.push(new Uint8Array(0), true);
      resolve();
    });
    stream.on("error", reject);
  });
}

/**
 * fflate 的流式 Zip writer：逐个对象从磁盘推进，不把整个媒体库或整包读进内存。
 * 对象本身已按类别压缩后再 AES-GCM 加密，因此 Zip 层只做 STORE。
 */
export async function writePortableBackup(
  destinationPath: string,
  sources: PortableSource[],
): Promise<void> {
  const output = fs.createWriteStream(destinationPath, { flags: "wx" });
  let settled = false;
  const completed = new Promise<void>((resolve, reject) => {
    output.on("error", reject);
    output.on("finish", () => {
      settled = true;
      resolve();
    });
    const zip = new Zip((error, data, final) => {
      if (error) {
        reject(error);
        output.destroy();
        return;
      }
      output.write(Buffer.from(data));
      if (final) output.end();
    });
    void (async () => {
      try {
        for (const source of sources) {
          if ("data" in source) addBuffer(zip, source.archivePath, source.data);
          else await addFile(zip, source.archivePath, source.filePath);
        }
        zip.end();
      } catch (error) {
        reject(error);
        output.destroy();
      }
    })();
  });
  try {
    await completed;
  } catch (error) {
    if (!settled) fs.rmSync(destinationPath, { force: true });
    throw error;
  }
}
