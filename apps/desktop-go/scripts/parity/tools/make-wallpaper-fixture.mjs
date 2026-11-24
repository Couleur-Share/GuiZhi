#!/usr/bin/env node
/**
 * 生成对拍用的合成壁纸夹具 viva/parity/testdata/wallpaper-fixture.png（640×360，16:9，确定性、无外部依赖）。
 *
 * 画面全部是低频内容——大块平色加软边形状，让同一张图能检验背景图管线的每一步（cover 居中裁剪、不透明度、虚化），又不放大已登记的结构性差距：
 *   - 底：四块大面积的平色（青 → 靛 → 紫 → 橙，沿对角线排开），块间只有约 56 像素宽的平滑过渡。刻意不用连续渐变：墨迹比较
 *     （compare_l2.go 的 inkProfile）把「与区域外圈众数的最大通道差」当墨迹，渐变底色上两个引擎各自的插值误差会被放大成被裁薄片文字行的墨迹错位；
 *   - 四个软边圆与一块软边圆角矩形（边缘 10px 的平滑过渡）：位置与大小刻意不对称，居中裁剪或缩放错位都会在像素上显出来；
 *   - 不放硬边（直线、三角、细线）也不贴边放高反差内容：预览舞台里的侧栏、工具栏、面板和整窗的左侧栏都是磨砂玻璃，渲染层对它们做
 *     backdrop-filter 模糊，Viva 没有这个原语（台账 A-007，夹具覆盖的缺口见 G-038）；玻璃下面的边缘越硬，这条差距在像素上放大得越多
 *     （2026-11-24 实测：24px 品红竖带让预览舞台的 L3 多出 0.6–1.0%）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/make-wallpaper-fixture.mjs
 * 改画面后要重新采集三个壁纸场景的 React 参照并重新导出 facts（见 viva-ui-development.md 的 M3-6c 一节）。
 */
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";

const W = 640;
const H = 360;
const OUT = path.join(import.meta.dirname, "../../../viva/parity/testdata/wallpaper-fixture.png");

const clamp01 = (v) => Math.min(1, Math.max(0, v));
const smooth = (a, b, x) => {
  const t = clamp01((x - a) / (b - a));
  return t * t * (3 - 2 * t);
};
const mix = (c0, c1, t) => c0.map((v, i) => v + (c1[i] - v) * t);
const quant = (v) => Math.round(Math.round(v / 8) * 8);

const CYAN = [38, 166, 190];
const PURPLE = [122, 84, 190];
const ORANGE = [238, 148, 64];
const INDIGO = [72, 104, 200];
const TONES = [CYAN, INDIGO, PURPLE, ORANGE];

const px = new Float64Array(W * H * 3);
function put(x, y, rgb) {
  px.set(rgb, (y * W + x) * 3);
}

for (let y = 0; y < H; y += 1) {
  for (let x = 0; x < W; x += 1) {
    const t = (x / W + y / H) / 2; // 对角方向 0..1
    // 三条过渡带的中心在 0.25 / 0.5 / 0.75，宽度 0.0875（对角距离，约 56 像素）
    let rgb = TONES[0];
    for (let k = 1; k < TONES.length; k += 1) rgb = mix(rgb, TONES[k], smooth(k * 0.25 - 0.04375, k * 0.25 + 0.04375, t));
    put(x, y, rgb);
  }
}

/** 软边圆：半径内整色，边缘 feather 像素内平滑过渡到底色。 */
function softCircle(cx, cy, r, feather, color) {
  for (let y = Math.max(0, Math.floor(cy - r - feather)); y < Math.min(H, Math.ceil(cy + r + feather)); y += 1) {
    for (let x = Math.max(0, Math.floor(cx - r - feather)); x < Math.min(W, Math.ceil(cx + r + feather)); x += 1) {
      const d = Math.hypot(x + 0.5 - cx, y + 0.5 - cy);
      const a = 1 - smooth(r - feather / 2, r + feather / 2, d);
      if (a <= 0) continue;
      const i = (y * W + x) * 3;
      put(x, y, mix([px[i], px[i + 1], px[i + 2]], color, a * 0.92));
    }
  }
}
/** 软边圆角矩形：矩形内整色，边缘 feather 像素内平滑过渡到底色（距离按圆角矩形的有符号距离算，r 是圆角半径）。 */
function softRect(x0, y0, x1, y1, r, feather, color) {
  const cx = (x0 + x1) / 2;
  const cy = (y0 + y1) / 2;
  const hx = (x1 - x0) / 2 - r;
  const hy = (y1 - y0) / 2 - r;
  for (let y = Math.max(0, Math.floor(y0 - feather)); y < Math.min(H, Math.ceil(y1 + feather)); y += 1) {
    for (let x = Math.max(0, Math.floor(x0 - feather)); x < Math.min(W, Math.ceil(x1 + feather)); x += 1) {
      const qx = Math.max(Math.abs(x + 0.5 - cx) - hx, 0);
      const qy = Math.max(Math.abs(y + 0.5 - cy) - hy, 0);
      const d = Math.hypot(qx, qy) - r; // 圆角矩形边界处为 0，向外为正
      const a = 1 - smooth(-feather / 2, feather / 2, d);
      if (a <= 0) continue;
      const i = (y * W + x) * 3;
      put(x, y, mix([px[i], px[i + 1], px[i + 2]], color, a * 0.92));
    }
  }
}

// 全部是软边，且都离开画面边缘（见文件头说明）：偏心的位置与大小不对称，居中裁剪或缩放错位都会在像素上显出来。
softCircle(205, 118, 72, 10, [250, 224, 96]);
softCircle(470, 250, 90, 10, [48, 120, 96]);
softCircle(345, 100, 56, 10, [236, 96, 128]);
softCircle(545, 140, 46, 10, [30, 34, 56]);
softRect(110, 236, 250, 300, 12, 10, [34, 40, 66]);
// 量化成 8 级色阶并按 PNG 的 RGB 8 位编码（逐行过滤类型 0 以外的由 zlib 之前的 Up 过滤处理）
const rows = [];
let prev = Buffer.alloc(W * 3);
for (let y = 0; y < H; y += 1) {
  const cur = Buffer.alloc(W * 3);
  for (let i = 0; i < W * 3; i += 1) cur[i] = Math.min(255, quant(px[y * W * 3 + i]));
  const filtered = Buffer.alloc(1 + W * 3);
  filtered[0] = 2; // Up
  for (let i = 0; i < W * 3; i += 1) filtered[1 + i] = (cur[i] - prev[i] + 256) & 255;
  rows.push(filtered);
  prev = cur;
}
const idat = zlib.deflateSync(Buffer.concat(rows), { level: 9 });

const crcTable = new Uint32Array(256).map((_, n) => {
  let c = n;
  for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
const crc32 = (buf) => {
  let c = 0xffffffff;
  for (const b of buf) c = crcTable[(c ^ b) & 255] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
};
const chunk = (type, data) => {
  const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const out = Buffer.alloc(8 + data.length + 4);
  out.writeUInt32BE(data.length, 0);
  body.copy(out, 4);
  out.writeUInt32BE(crc32(body), 8 + data.length);
  return out;
};
const ihdr = Buffer.alloc(13);
ihdr.writeUInt32BE(W, 0);
ihdr.writeUInt32BE(H, 4);
ihdr[8] = 8; // 位深
ihdr[9] = 2; // RGB
const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  chunk("IHDR", ihdr),
  chunk("IDAT", idat),
  chunk("IEND", Buffer.alloc(0)),
]);
fs.mkdirSync(path.dirname(OUT), { recursive: true });
fs.writeFileSync(OUT, png);
console.log(`${path.relative(process.cwd(), OUT)}：${W}x${H}，${(png.length / 1024).toFixed(1)} KB`);
