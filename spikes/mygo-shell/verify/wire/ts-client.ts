// TS 版抓取层的线上字节采样客户端（产品原文件 safe-fetch.ts，经桩换掉目标校验后打包运行）。
// 用法：<运行时> ts-client.cjs <明文端口> <TLS 端口>；请求与 wire_client_test.go 一一对应。
import fs from "node:fs";
import { downloadToTempFile, fetchRawText } from "../../../../apps/desktop/src/main/services/import/safe-fetch";

const [plain, tlsPort] = process.argv.slice(2);
if (!plain || !tlsPort) throw new Error("用法: ts-client <明文端口> <TLS 端口>");
const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";
const topic = "37194262";
const q = `/read.php?tid=${topic}&page=1&lite=js`;

async function main() {

await fetchRawText(`http://bbs.nga.cn:${plain}${q}`, {
  userAgent: UA, referer: `https://bbs.nga.cn/read.php?tid=${topic}`, cookie: "guestJs=abc", acceptStatuses: [403],
});
const dl = await downloadToTempFile(`http://img.nga.cn:${plain}/attachments/x.jpg`, {
  userAgent: UA, referer: "https://bbs.nga.cn/", maxBytes: 1 << 20, fileName: "image.bin",
});
fs.rmSync(dl.dir, { recursive: true, force: true });
// TLS：握手必然失败（服务器读完 ClientHello 就断开）
await fetchRawText(`https://bbs.nga.cn:${tlsPort}${q}`, {
  userAgent: UA, referer: `https://bbs.nga.cn/read.php?tid=${topic}`, acceptStatuses: [403],
}).catch(() => undefined);

}
main().catch((e) => { console.error(e); process.exit(1); });
