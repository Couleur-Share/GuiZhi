// 生成「主题阅读视图往返」夹具：在一份库副本里加一个条目 + 一个 v3 阅读版本（含真实 PNG 图片素材与测试脚本）。
// 页面内脚本（design.scripts）与固定的阅读脚本在同一个文档里运行，所以它可以在自己的 document 上合成用户事件
// （mouseup 选区、Ctrl+F、Esc、点击图片），让固定脚本经 parent.postMessage 汇报；这样能走通
// 「iframe → shim 中继 → Go 校验 → 推回渲染进程」整条链路。注意：合成事件不是真实鼠标键盘输入。
//   用法：tsx make-reading-fixture.mts <源库> <输出 userData 目录>
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { initDatabase, closeDatabase, KnowledgeItemDB, ThemedReadingDB } from "../../../packages/db/src/index.ts";
import { v3Page } from "../../../spikes/mygo-db/golden-themed.mts";

const [srcDb, ud] = process.argv.slice(2);
if (!srcDb || !ud) throw new Error("用法：make-reading-fixture.mts <源库> <输出 userData 目录>");

const crc = (() => { const t: number[] = []; for (let n = 0; n < 256; n++) { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; t[n] = c >>> 0; } return (b: Buffer) => { let c = 0xffffffff; for (const x of b) c = t[(c ^ x) & 255] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0; }; })();
function png(w: number, h: number, rgb: [number, number, number]): Buffer {
  const chunk = (type: string, data: Buffer) => { const len = Buffer.alloc(4); len.writeUInt32BE(data.length); const td = Buffer.concat([Buffer.from(type), data]); const c = Buffer.alloc(4); c.writeUInt32BE(crc(td)); return Buffer.concat([len, td, c]); };
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
  const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: w }, () => rgb).flat())]);
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk("IHDR", ihdr), chunk("IDAT", zlib.deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]);
}

const dataDir = path.join(ud, "data");
fs.rmSync(ud, { recursive: true, force: true });
fs.mkdirSync(path.join(dataDir, "assets", "images"), { recursive: true });
fs.copyFileSync(srcDb, path.join(dataDir, "knowledge.db"));
const image = png(320, 180, [54, 143, 189]);
const imageName = "rv-img1.png";
fs.writeFileSync(path.join(dataDir, "assets", "images", imageName), image);

const db = initDatabase(path.join(dataDir, "knowledge.db"));
const items = new KnowledgeItemDB(db);
const title = "阅读视图往返验证";
const paragraphs = Array.from({ length: 30 }, (_, i) => `第${i + 1}段：用于验证滚动、查找与选区的正文。关键词 GZ-FIND 出现在每一段里，便于统计命中数；本段编号 ${i + 1}。`);
const content = paragraphs.join("\n\n");
const item = items.create({ title, content, tagNames: [] } as never);
const fingerprint = crypto.createHash("sha256").update(JSON.stringify([title, content, null, "body"])).digest("hex");

// 页面脚本：在自己的文档里按时间表合成用户事件（定时器从脚本加载后开始）。
const testScript = `
const at=(ms,fn)=>setTimeout(()=>{try{fn()}catch(e){window.dispatchEvent(new CustomEvent('reading-script-fault',{detail:{id:'rv-probe',message:String(e.message||e)}}))}},ms);
at(2500,()=>{const p=document.getElementById('target');const r=document.createRange();r.selectNodeContents(p);const s=getSelection();s.removeAllRanges();s.addRange(r);document.dispatchEvent(new MouseEvent('mouseup',{bubbles:true}))});
at(3500,()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'f',ctrlKey:true,bubbles:true,cancelable:true})));
at(4500,()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})));
at(5500,()=>document.querySelector('img[data-theme-asset]').click());
`;
const html = `<main><h1 id="top">${title}</h1>${paragraphs.map((p, i) => (i === 0 ? `<h2 id="s1">第一节</h2><p id="target">${p}</p><img data-theme-asset="img1" alt="测试图" width="320" height="180">` : i === 15 ? `<h2 id="s2">第二节</h2><p>${p}</p>` : `<p>${p}</p>`)).join("")}</main>`;
const asset = { id: "img1", role: "original", purpose: "p", prompt: "", alt: "测试图", aspectRatio: "16:9", status: "ready", fileName: imageName, sha256: crypto.createHash("sha256").update(image).digest("hex"), bytes: image.length };

const page: any = v3Page({ id: "rv-1", itemId: item.id, sourceKind: "body", role: "current", createdAt: 100, updatedAt: 100, assets: [asset] }, {
  html, css: "", assets: [asset], scripts: [{ id: "rv-probe", code: testScript, status: "ready" }],
});
page.source = { ...page.source, title, content, sourceUri: null, fingerprint };
page.reconstruction.draft = [{ title: "完整正文", markdown: content, referenceIds: [] }];
page.reconstruction.visuals ??= [];
page.reconstruction.animations ??= [];
page.generation = { route: "short", sections: [{ id: "s1", html }], css: "", scripts: [], libraries: [], revision: 1, done: true, repairs: {}, issues: [] };
new ThemedReadingDB(db).saveVersion(page);
closeDatabase();
console.log(JSON.stringify({ itemId: item.id, versionId: "rv-1", title, userData: ud }));
