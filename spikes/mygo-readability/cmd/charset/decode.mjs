// Node 侧：对 cmd/charset gen 生成的输入，用产品同款 TextDecoder(label,{fatal:true}) 解码。
import fs from "node:fs";
const [inp, outp] = process.argv.slice(2);
const cases = JSON.parse(fs.readFileSync(inp, "utf8"));
const res = cases.map(({ label, hex }) => {
  try {
    return { ok: true, text: new TextDecoder(label, { fatal: true }).decode(Buffer.from(hex, "hex")) };
  } catch {
    return { ok: false, text: "" };
  }
});
fs.writeFileSync(outp, JSON.stringify(res));
console.log("Node 解码", res.length, "例，成功", res.filter((r) => r.ok).length);
