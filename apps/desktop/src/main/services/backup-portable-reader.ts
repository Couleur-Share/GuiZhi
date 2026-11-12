import fs from "node:fs";
import fsp from "node:fs/promises";
import path from "node:path";

/**
 * 便携备份（`.guizhi-backup`）的读取端。
 *
 * 写入端 `backup-portable.ts` 用 fflate 流式写 STORE-only 的 ZIP（带数据描述符），
 * 这里按中央目录读取：先读目录得到每个条目的偏移与大小，再逐个条目流式复制到暂存目录，
 * 不把整包读进内存，也不依赖本地文件头里的大小字段。
 *
 * 只接受白名单内的条目名（见 {@link isAllowedPortableEntry}）并拒绝加密、压缩或重名的条目，
 * 所以包内无法通过 `..`、绝对路径等把文件写到暂存目录之外。
 */

const SIG_EOCD = 0x06054b50;
const SIG_EOCD64_LOCATOR = 0x07064b50;
const SIG_EOCD64 = 0x06064b50;
const SIG_CENTRAL = 0x02014b50;
const SIG_LOCAL = 0x04034b50;
const COPY_CHUNK_BYTES = 1024 * 1024;
const MAX_ENTRIES = 5_000_000;

export interface PortableArchiveEntry {
  name: string;
  size: number;
  crc32: number;
  localOffset: number;
}

const OBJECT_ENTRY = /^objects\/[a-f0-9]{64}$/;
const FIXED_ENTRIES = new Set([
  "repository-header.json",
  "snapshot/header.json",
  "snapshot/manifest.enc",
]);

export function isAllowedPortableEntry(name: string): boolean {
  return FIXED_ENTRIES.has(name) || OBJECT_ENTRY.test(name);
}

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n += 1) {
    let c = n;
    for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c >>> 0;
  }
  return table;
})();

/** 增量 CRC-32；初值传 0，返回值可继续作为下一块的初值。 */
export function crc32Update(previous: number, chunk: Uint8Array): number {
  let crc = ~previous >>> 0;
  for (let i = 0; i < chunk.length; i += 1) {
    crc = CRC_TABLE[(crc ^ chunk[i]) & 0xff] ^ (crc >>> 8);
  }
  return ~crc >>> 0;
}

function readU64(buffer: Buffer, offset: number): number {
  const value = buffer.readBigUInt64LE(offset);
  if (value > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error("便携备份的条目偏移或大小超出支持范围");
  }
  return Number(value);
}

async function readAt(
  handle: fsp.FileHandle,
  position: number,
  length: number,
): Promise<Buffer> {
  const buffer = Buffer.alloc(length);
  let filled = 0;
  while (filled < length) {
    const { bytesRead } = await handle.read(
      buffer,
      filled,
      length - filled,
      position + filled,
    );
    if (bytesRead === 0) throw new Error("便携备份文件被截断或不完整");
    filled += bytesRead;
  }
  return buffer;
}

async function locateCentralDirectory(
  handle: fsp.FileHandle,
  fileSize: number,
): Promise<{ offset: number; entries: number }> {
  const tailLength = Math.min(fileSize, 22 + 0xffff);
  const tailStart = fileSize - tailLength;
  const tail = await readAt(handle, tailStart, tailLength);
  let eocd = -1;
  for (let i = tail.length - 22; i >= 0; i -= 1) {
    if (tail.readUInt32LE(i) === SIG_EOCD) {
      eocd = i;
      break;
    }
  }
  if (eocd < 0) throw new Error("不是有效的归知便携备份（找不到 ZIP 目录）");
  let entries = tail.readUInt16LE(eocd + 10);
  let offset = tail.readUInt32LE(eocd + 16);
  const cdSize = tail.readUInt32LE(eocd + 12);
  if (entries === 0xffff || offset === 0xffffffff || cdSize === 0xffffffff) {
    const locatorAt = eocd - 20;
    if (locatorAt < 0 || tail.readUInt32LE(locatorAt) !== SIG_EOCD64_LOCATOR) {
      throw new Error("便携备份的 ZIP64 目录定位记录缺失");
    }
    const eocd64Offset = readU64(tail, locatorAt + 8);
    const eocd64 = await readAt(handle, eocd64Offset, 56);
    if (eocd64.readUInt32LE(0) !== SIG_EOCD64) {
      throw new Error("便携备份的 ZIP64 目录损坏");
    }
    entries = readU64(eocd64, 32);
    offset = readU64(eocd64, 48);
  }
  if (entries > MAX_ENTRIES) throw new Error("便携备份的条目数量异常");
  return { offset, entries };
}

/** 读取并校验中央目录。不合规的条目整体拒绝，不跳过。 */
export async function listPortableArchive(
  handle: fsp.FileHandle,
  fileSize: number,
): Promise<PortableArchiveEntry[]> {
  const cd = await locateCentralDirectory(handle, fileSize);
  const entries: PortableArchiveEntry[] = [];
  const seen = new Set<string>();
  let position = cd.offset;
  for (let index = 0; index < cd.entries; index += 1) {
    const fixed = await readAt(handle, position, 46);
    if (fixed.readUInt32LE(0) !== SIG_CENTRAL) {
      throw new Error("便携备份的 ZIP 目录损坏");
    }
    const flags = fixed.readUInt16LE(8);
    const method = fixed.readUInt16LE(10);
    const crc = fixed.readUInt32LE(16);
    let compressed = fixed.readUInt32LE(20);
    let uncompressed = fixed.readUInt32LE(24);
    const nameLength = fixed.readUInt16LE(28);
    const extraLength = fixed.readUInt16LE(30);
    const commentLength = fixed.readUInt16LE(32);
    let localOffset = fixed.readUInt32LE(42);
    const variable = await readAt(handle, position + 46, nameLength + extraLength);
    const name = variable.subarray(0, nameLength).toString("utf8");
    position += 46 + nameLength + extraLength + commentLength;

    if (name.endsWith("/") && uncompressed === 0) continue; // 目录条目：没有内容，也不会被写出
    if (!isAllowedPortableEntry(name)) {
      throw new Error(`便携备份包含无法识别的文件：${name.slice(0, 80)}`);
    }
    if (seen.has(name)) throw new Error(`便携备份中的文件重复：${name}`);
    seen.add(name);
    if (flags & 0x1) throw new Error("便携备份的条目带有 ZIP 自身的加密，格式不受支持");
    if (method !== 0) throw new Error(`便携备份的条目使用了压缩（${name}），格式不受支持`);

    if (compressed === 0xffffffff || uncompressed === 0xffffffff || localOffset === 0xffffffff) {
      const extra = variable.subarray(nameLength);
      let cursor = 0;
      let found = false;
      while (cursor + 4 <= extra.length) {
        const id = extra.readUInt16LE(cursor);
        const size = extra.readUInt16LE(cursor + 2);
        if (id === 0x0001) {
          let at = cursor + 4;
          if (uncompressed === 0xffffffff) { uncompressed = readU64(extra, at); at += 8; }
          if (compressed === 0xffffffff) { compressed = readU64(extra, at); at += 8; }
          if (localOffset === 0xffffffff) { localOffset = readU64(extra, at); }
          found = true;
          break;
        }
        cursor += 4 + size;
      }
      if (!found) throw new Error(`便携备份的 ZIP64 条目信息缺失：${name}`);
    }
    if (compressed !== uncompressed) {
      throw new Error(`便携备份的条目大小不一致：${name}`);
    }
    entries.push({ name, size: uncompressed, crc32: crc, localOffset });
  }
  return entries;
}

async function copyEntry(
  handle: fsp.FileHandle,
  entry: PortableArchiveEntry,
  destination: string,
): Promise<void> {
  const local = await readAt(handle, entry.localOffset, 30);
  if (local.readUInt32LE(0) !== SIG_LOCAL) {
    throw new Error(`便携备份的文件头损坏：${entry.name}`);
  }
  const dataStart = entry.localOffset + 30 + local.readUInt16LE(26) + local.readUInt16LE(28);
  await fsp.mkdir(path.dirname(destination), { recursive: true });
  const output = await fsp.open(destination, "wx");
  try {
    let remaining = entry.size;
    let position = dataStart;
    let crc = 0;
    while (remaining > 0) {
      const length = Math.min(COPY_CHUNK_BYTES, remaining);
      const chunk = await readAt(handle, position, length);
      crc = crc32Update(crc, chunk);
      await output.write(chunk);
      position += length;
      remaining -= length;
    }
    if (crc !== entry.crc32) {
      throw new Error(`便携备份文件校验失败（CRC 不一致）：${entry.name}`);
    }
  } finally {
    await output.close();
  }
}

/**
 * 把便携备份展开到 `stagingDir`（必须是空的、专用的暂存目录），返回展开的条目名。
 * 任何一个条目不合规或校验失败都会抛错；调用方负责清理暂存目录。
 */
export async function extractPortableArchive(
  archivePath: string,
  stagingDir: string,
): Promise<string[]> {
  const stat = fs.statSync(archivePath);
  if (!stat.isFile()) throw new Error("选择的不是文件");
  if (stat.size < 22) throw new Error("不是有效的归知便携备份（文件过小）");
  const handle = await fsp.open(archivePath, "r");
  try {
    const entries = await listPortableArchive(handle, stat.size);
    for (const entry of entries) {
      await copyEntry(handle, entry, path.join(stagingDir, ...entry.name.split("/")));
    }
    return entries.map((entry) => entry.name);
  } finally {
    await handle.close();
  }
}
