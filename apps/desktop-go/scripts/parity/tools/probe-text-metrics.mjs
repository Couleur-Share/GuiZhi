#!/usr/bin/env node
/**
 * 在 React 参照（设备缩放钉死为 1）里用页外探针量 Chromium 的文字度量，生成 Go 侧的守卫夹具。
 *
 * 对每组（字号, 字重, 行高）量出：内容区高度、内容区顶（相对行盒顶）、基线（相对行盒顶）。
 * 探针元素放在 left:-9999px，测完即删，不碰页面真实内容。
 * 夹具用来钉死 viva/kit/baseline.go 里 ChromeBaselineRow 的模型：模型与夹具不符时 Go 测试会红。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/tools/probe-text-metrics.mjs [--out <文件>] [--sizes 10.5,12,...] [--lines 14,17.5,...]
 *
 * 缺省输出到 apps/desktop-go/viva/kit/testdata/chrome-text-probe.json。
 * 前提：已有 React 参照构建（node apps/desktop-go/scripts/parity/build-react-ref.mjs）。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { REPO_ROOT } from "../lib/paths.mjs";
import { openReactSession } from "../lib/react-session.mjs";

// 覆盖三档根字号（14/16/18px）下 Tailwind 文字尺寸的字号与行高：小数字号（10.5、12.25、15.75、13.5、20.25）与小数行高（17.5、24.5、22.5、31.5）都在内。
const DEFAULT_SIZES = [9, 10.5, 12, 12.25, 13.5, 14, 15.75, 18, 20.25];
const DEFAULT_LINES = [14, 16, 17.5, 18, 20, 22.5, 24.5, 28, 31.5];
const WEIGHTS = [400, 500, 600, 700];

function parseArgs(argv) {
  const args = {
    out: path.join(REPO_ROOT, "apps/desktop-go/viva/kit/testdata/chrome-text-probe.json"),
    sizes: DEFAULT_SIZES,
    lines: DEFAULT_LINES,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const need = () => {
      if (argv[i + 1] === undefined) throw new Error(`${argv[i]} 缺少值`);
      return argv[(i += 1)];
    };
    switch (argv[i]) {
      case "--out": args.out = path.resolve(need()); break;
      case "--sizes": args.sizes = need().split(",").map(Number); break;
      case "--lines": args.lines = need().split(",").map(Number); break;
      default: throw new Error(`未知参数 ${argv[i]}`);
    }
  }
  return args;
}

const args = parseArgs(process.argv.slice(2));
const record = readBuildRecord();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
try {
  const { samples, chromium } = await session.page.evaluate(({ sizes, weights, lines }) => {
    const family = getComputedStyle(document.body).fontFamily;
    const measure = (size, weight, lh) => {
      const div = document.createElement("div");
      div.style.cssText = `position:absolute;left:-9999px;top:0;font-family:${family};font-size:${size}px;font-weight:${weight};line-height:${lh}px;white-space:nowrap`;
      div.innerHTML = '<span class="t">汉字Aa</span><span class="p" style="display:inline-block;width:0;height:0"></span>';
      document.body.appendChild(div);
      const t = div.querySelector(".t").getBoundingClientRect();
      const p = div.querySelector(".p").getBoundingClientRect();
      const d = div.getBoundingClientRect();
      const sample = { size, weight, line: lh, contentTop: t.y - d.y, contentHeight: t.height, baseline: p.bottom - d.y };
      div.remove();
      return sample;
    };
    const out = [];
    for (const s of sizes) for (const w of weights) for (const lh of lines) out.push(measure(s, w, lh));
    return { samples: out, chromium: navigator.userAgent.match(/Edg\/([\d.]+)/)?.[1] ?? navigator.userAgent };
  }, { sizes: args.sizes, weights: WEIGHTS, lines: args.lines });

  const doc = {
    note: "由 scripts/parity/tools/probe-text-metrics.mjs 生成；设备缩放钉死为 1；各值单位为 CSS 像素，相对行盒顶。",
    chromium,
    fontFamily: "Microsoft YaHei UI（系统字体栈首选）",
    samples,
  };
  fs.mkdirSync(path.dirname(args.out), { recursive: true });
  fs.writeFileSync(args.out, `${JSON.stringify(doc, null, 1)}\n`);
  console.log(`已写 ${samples.length} 组样本到 ${args.out}（Edge ${chromium}）`);
} finally {
  await session.close();
}
