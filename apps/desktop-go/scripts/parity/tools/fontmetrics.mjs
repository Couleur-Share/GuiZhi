#!/usr/bin/env node
/**
 * 读 TTC/TTF 的 head/hhea/OS-2 表，打印各字体的垂直度量（JSON，每行一个字体）。
 * 用来核对 viva/kit/baseline.go 里 face 的 ascent/descent 常数：
 *   node apps/desktop-go/scripts/parity/tools/fontmetrics.mjs C:\Windows\Fonts\msyh.ttc
 *   （msyhsb.ttc 是 Semibold，msyhbd.ttc 是 Bold）
 */
import { readFileSync } from "node:fs";

const file = process.argv[2];
const buf = readFileSync(file);
const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
const tag = (o) => String.fromCharCode(buf[o], buf[o + 1], buf[o + 2], buf[o + 3]);

function fontOffsets() {
  if (tag(0) === "ttcf") {
    const n = dv.getUint32(8);
    return Array.from({ length: n }, (_, i) => dv.getUint32(12 + 4 * i));
  }
  return [0];
}

function tables(off) {
  const num = dv.getUint16(off + 4);
  const t = {};
  for (let i = 0; i < num; i++) {
    const rec = off + 12 + 16 * i;
    t[tag(rec)] = { offset: dv.getUint32(rec + 8), length: dv.getUint32(rec + 12) };
  }
  return t;
}

function readName(t, id) {
  const nt = t["name"];
  if (!nt) return "";
  const count = dv.getUint16(nt.offset + 2);
  const strOff = dv.getUint16(nt.offset + 4);
  for (let i = 0; i < count; i++) {
    const r = nt.offset + 6 + 12 * i;
    const platform = dv.getUint16(r), nameId = dv.getUint16(r + 6);
    if (nameId !== id) continue;
    const len = dv.getUint16(r + 8), o = dv.getUint16(r + 10);
    const start = nt.offset + strOff + o;
    if (platform === 3 || platform === 0) {
      let s = "";
      for (let k = 0; k < len; k += 2) s += String.fromCharCode(dv.getUint16(start + k));
      return s;
    }
  }
  return "";
}

fontOffsets().forEach((off, idx) => {
  const t = tables(off);
  const head = t["head"].offset;
  const upem = dv.getUint16(head + 18);
  const hhea = t["hhea"].offset;
  const os2 = t["OS/2"].offset;
  const fsSel = dv.getUint16(os2 + 62);
  const out = {
    idx,
    family: readName(t, 1),
    sub: readName(t, 2),
    full: readName(t, 4),
    upem,
    hheaAscender: dv.getInt16(hhea + 4),
    hheaDescender: dv.getInt16(hhea + 6),
    hheaLineGap: dv.getInt16(hhea + 8),
    typoAscender: dv.getInt16(os2 + 68),
    typoDescender: dv.getInt16(os2 + 70),
    typoLineGap: dv.getInt16(os2 + 72),
    winAscent: dv.getUint16(os2 + 74),
    winDescent: dv.getUint16(os2 + 76),
    useTypoMetrics: !!(fsSel & (1 << 7)),
    weightClass: dv.getUint16(os2 + 4),
  };
  console.log(JSON.stringify(out));
});
