// 生成 Go 流式 UTF-8 解码器的 golden：随机字节（偏向 UTF-8 的前导/后续字节与合法序列）、随机分块，
// 期望值由 Node 的 TextDecoder（stream 模式，与 ai.ipc.ts 同用法）给出。
// 用法：node make-utf8-golden.mjs <输出.json>
import fs from "node:fs";
let seed = 20261111;
const rnd = () => ((seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0) / 4294967296);
const pick = (a) => a[Math.floor(rnd() * a.length)];
const specials = [0x00, 0x41, 0x7f, 0x80, 0xbf, 0xc0, 0xc1, 0xc2, 0xdf, 0xe0, 0xe1, 0xed, 0xef, 0xf0, 0xf1, 0xf4, 0xf5, 0xff, 0x9f, 0xa0, 0x8f, 0x90];
const valid = ["a", "é", "你", "€", "🙂", "\u{10ffff}", "\ufeff", "\ud7ff", "\ue000"].map((s) => [...Buffer.from(s)]);
const cases = [];
for (let n = 0; n < 600; n++) {
  const bytes = [];
  const len = Math.floor(rnd() * 24);
  while (bytes.length < len) {
    const r = rnd();
    if (r < 0.45) bytes.push(...pick(valid));
    else if (r < 0.8) bytes.push(pick(specials));
    else bytes.push(Math.floor(rnd() * 256));
  }
  // 随机切块（可能切在多字节序列中间，也可能出现空块）
  const chunks = [];
  for (let i = 0; i < bytes.length; ) {
    const k = Math.floor(rnd() * 5);
    chunks.push(bytes.slice(i, i + k));
    i += k;
    if (k === 0 && rnd() < 0.5) i += 1, chunks.push([bytes[i - 1]]);
  }
  const dec = new TextDecoder();
  let out = "";
  for (const c of chunks) out += dec.decode(Uint8Array.from(c), { stream: true });
  out += dec.decode();
  cases.push({ chunks: chunks.map((c) => Buffer.from(c).toString("hex")), out });
}
fs.writeFileSync(process.argv[2], JSON.stringify(cases));
console.log("cases", cases.length);
