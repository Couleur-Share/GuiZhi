#!/usr/bin/env node
/**
 * 在 React 参照（Chromium）里量 white-space: normal 下哪些字符会折叠成一个空格（台账 B-041 收口时发现 TextNoKern / TextMarked 用的
 * unicode.IsSpace 比 Chromium 的折叠集合宽）：对每个候选字符 c，量「a c c b」的宽度，与「a b」（一个空格）、「a c b」比较。
 *
 * 实测结果（14px，Chromium）：空格、制表符、换行、回车折成一个空格；换页、竖制表符、不换行空格 U+00A0、全角空格 U+3000、
 * U+2003 / U+2009 等各种宽度的空格、U+0085、行分隔符与段分隔符都不折叠；U+200B 宽度为 0。对应 viva/kit 的 isCollapsibleSpace。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-collapse.mjs
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import { readBuildRecord } from "../build-react-ref.mjs";
import { openReactSession } from "../lib/react-session.mjs";

const record = readBuildRecord();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
try {
  const win = session.page;
  const out = await win.evaluate(() => {
    const cases = {
      space: " ", tab: "\t", lf: "\n", cr: "\r", ff: "\f", vt: "\v", nbsp: "\u00a0", ideographic: "\u3000",
      emsp: "\u2003", thin: "\u2009", zwsp: "\u200b", nel: "\u0085", ls: "\u2028", ps: "\u2029",
    };
    const host = document.createElement("div");
    host.style.cssText = "position:fixed;left:0;top:0;visibility:hidden;font:14px ui-sans-serif,system-ui,sans-serif;white-space:normal;width:2000px";
    document.body.appendChild(host);
    const width = (text) => {
      const s = document.createElement("span");
      s.textContent = text;
      host.appendChild(s);
      const w = s.getBoundingClientRect().width;
      host.removeChild(s);
      return Math.round(w * 1000) / 1000;
    };
    const one = width("a b");
    const res = { "a b（一个空格）": one, ab: width("ab") };
    for (const [name, c] of Object.entries(cases)) {
      const two = width(`a${c}${c}b`);
      const single = width(`a${c}b`);
      res[name] = { double: two, single, collapsesToOneSpace: Math.abs(two - one) < 0.01 };
    }
    document.body.removeChild(host);
    return res;
  });
  console.log(JSON.stringify(out, null, 1));
} finally {
  await session.close();
}
