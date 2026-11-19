#!/usr/bin/env node
/**
 * 在 React 参照里量「省略号落在命中词（mark）起点或内部」时 Chromium 怎么画（台账 B-041）：
 * 底色画到哪、省略号的起点与颜色，随容器宽度逐像素扫描。结论写在 viva/kit/marktext.go 的头注释里：
 *   - 被省略号盖掉的文字不画，但行内盒与它的底色还在原位置（超出容器右缘的部分被 overflow 裁平）；
 *   - 省略号永远是容器（宿主）的字色，落在命中词里或紧挨命中词时也不是 amber-950；
 *   - 省略号从前缀「文字」的右缘起画，前缀末尾命中词的右内边距不算（省略号压在内边距上）；
 *   - 加省略号放得下才留字：「前缀文字右缘 + 省略号宽度 ≤ 容器宽」，右内边距不计入。
 *
 * 做法：在 React 会话页面里放一个定宽的容器（truncate 或 -webkit-line-clamp），前缀文字设成透明、省略号保持容器字色（蓝），
 * 命中词用与 highlight-text.tsx 相同的类名；截一张图，在页内解码后按颜色分类统计：琥珀底色（amber）、命中词的字色（brown）、
 * 省略号的墨迹（blue，因为前缀透明，蓝色只可能是省略号）各自的横向范围，blueCols 是省略号墨迹的列号（相对容器左缘）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-mark-ellipsis.mjs [--mode truncate|clamp] [--tail plain|mark|none]
 *       [--m1 设计设计] [--mid 好好] [--from 148] [--to 172] [--step 1]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import { readBuildRecord } from "../build-react-ref.mjs";
import { openReactSession } from "../lib/react-session.mjs";
const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const mode = arg("--mode", "truncate");
const tail = arg("--tail", "plain");
const m1Text = arg("--m1", "设计设计");
const midText = arg("--mid", "好好");
const from = Number(arg("--from", "148"));
const to = Number(arg("--to", "172"));
const step = Number(arg("--step", "1"));

const record = readBuildRecord();
const session = await openReactSession({ executable: record.exe, dist: record.dist });

// 页内：把 PNG（base64）解码后按颜色类别统计每一类像素的横向范围（相对容器左缘）。
function scan({ b64, left, bandTop, bandBottom }) {
  return (async () => {
    const bin = atob(b64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
    const bmp = await createImageBitmap(new Blob([arr], { type: "image/png" }));
    const c = new OffscreenCanvas(bmp.width, bmp.height);
    const g = c.getContext("2d");
    g.drawImage(bmp, 0, 0);
    const { data, width } = g.getImageData(0, 0, bmp.width, bmp.height);
    const range = { amber: [1e9, -1], brown: [1e9, -1], blue: [1e9, -1] };
    const blueCols = new Set();
    const hit = (k, x) => {
      range[k][0] = Math.min(range[k][0], x - left);
      range[k][1] = Math.max(range[k][1], x - left);
    };
    for (let y = bandTop; y < bandBottom; y++) {
      for (let x = 0; x < width; x++) {
        const o = (y * width + x) * 4;
        const r = data[o], gg = data[o + 1], b = data[o + 2];
        if (r > 235 && gg > 170 && gg < 225 && b > 30 && b < 120) hit("amber", x);
        else if (b - r > 90 && b > 150) { hit("blue", x); blueCols.add(x - left); }
        else if (r < 150 && gg < 110 && b < 80 && r - b > 15) hit("brown", x);
      }
    }
    range.blueCols = [...blueCols].sort((p, q) => p - q);
    return range;
  })();
}

try {
  const win = session.page;
  const rows = [];
  for (let w = from; w <= to; w += step) {
    const info = await win.evaluate(
      ({ w, mode, tail, m1Text, midText }) => {
        document.getElementById("probe-host")?.remove();
        const host = document.createElement("div");
        host.id = "probe-host";
        host.style.cssText = "position:fixed;left:20px;top:20px;width:520px;z-index:99999;background:#fff;padding:10px 0";
        const box = document.createElement("div");
        const clamp = mode === "clamp";
        box.style.cssText =
          `width:${w}px;font-size:14px;line-height:20px;font-weight:400;color:rgb(0,0,255);font-family:ui-sans-serif,system-ui,sans-serif,"Apple Color Emoji","Segoe UI Emoji";` +
          (clamp
            ? "overflow:hidden;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:1;"
            : "overflow:hidden;text-overflow:ellipsis;white-space:nowrap;");
        const m = '<mark class="rounded-sm bg-amber-400/90 px-0.5 text-amber-950">' + m1Text + '</mark>';
        const m2 = '<mark class="rounded-sm bg-amber-400/90 px-0.5 text-amber-950">好</mark>';
        const ghost = (s) => `<span style="color:transparent">${s}</span>`;
        box.innerHTML = ghost("好好好好好好好好好好") + m + (tail === "plain" ? ghost("好好好好好好好好") : tail === "mark" ? ghost(midText) + m2 + ghost("好好好好好好") : "");
        host.appendChild(box);
        document.body.appendChild(host);
        const round = (n) => Math.round(n * 100) / 100;
        const hb = host.getBoundingClientRect();
        const mark = box.querySelector("mark");
        const r = document.createRange();
        r.selectNodeContents(mark);
        const rr = r.getClientRects()[0];
        const mb = mark.getClientRects()[0];
        return {
          left: hb.left,
          top: hb.top,
          boxH: box.getBoundingClientRect().height,
          boxTop: box.getBoundingClientRect().top,
          markText: [round(rr.left - hb.left), round(rr.width)],
          markBox: [round(mb.left - hb.left), round(mb.width)],
        };
      },
      { w, mode, tail, m1Text, midText },
    );
    const buf = await win.screenshot({ clip: { x: info.left - 4, y: info.top - 4, width: 520 + 8, height: 20 + 20 + 8 } });
    const left = 4;
    const top = info.boxTop - info.top + 4;
    const range = await win.evaluate(scan, { b64: buf.toString("base64"), left, bandTop: top + 2, bandBottom: top + 18 });
    rows.push({ w, ...info, range });
    const f = (a) => (a[1] < 0 ? "—" : `${a[0]}…${a[1]}`);
    console.log(
      `W=${w} 自然：mark 文字区 ${info.markText[0]}+${info.markText[1]}，盒 ${info.markBox[0]}+${info.markBox[1]} | 底色 ${f(range.amber)} 棕字 ${f(range.brown)} 蓝字 ${f(range.blue)} 蓝列 ${range.blueCols.join(",")}`,
    );
  }
} finally {
  await session.close();
}
