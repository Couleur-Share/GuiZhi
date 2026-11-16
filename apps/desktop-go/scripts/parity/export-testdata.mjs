#!/usr/bin/env node
/**
 * 把一批 React 参照的事实 JSON 压缩后导出到 Go 单元测试的入库目录。
 *
 * 入库的只有事实（压缩 JSON，不含 PNG 与 ARIA 文本）：Go 测试用它们做 L1 结构与 L2 度量的比较，
 * 不需要拉起 WebView2，也不依赖 D:\Project\GuiZhi-bench 里的参照。像素比较（L3）只在 vivaparity 里做。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/export-testdata.mjs --scenes settings.general --variants light-royal-blue-medium-1280x800,...
 *        [--from <React 参照目录>] [--to <目标目录>]
 *
 * 缺省 --from 是 <基准目录>/parity/react，--to 是 apps/desktop-go/viva/parity/testdata/react。
 * 只接受带 forcedDsf 标记的事实（设备缩放钉死为 1 的采集器产物）。
 */
import fs from "node:fs";
import path from "node:path";
import { parityOutDir, REPO_ROOT } from "./lib/paths.mjs";

function parseArgs(argv) {
  const args = {
    scenes: [],
    variants: [],
    from: path.join(parityOutDir(), "react"),
    to: path.join(REPO_ROOT, "apps/desktop-go/viva/parity/testdata/react"),
  };
  for (let i = 0; i < argv.length; i += 1) {
    const need = () => {
      if (argv[i + 1] === undefined) throw new Error(`${argv[i]} 缺少值`);
      return argv[(i += 1)];
    };
    switch (argv[i]) {
      case "--scenes": args.scenes = need().split(",").filter(Boolean); break;
      case "--variants": args.variants = need().split(",").filter(Boolean); break;
      case "--from": args.from = path.resolve(need()); break;
      case "--to": args.to = path.resolve(need()); break;
      default: throw new Error(`未知参数 ${argv[i]}`);
    }
  }
  if (!args.scenes.length || !args.variants.length) throw new Error("需要 --scenes 与 --variants");
  return args;
}

const args = parseArgs(process.argv.slice(2));
let bytes = 0;
for (const variant of args.variants) {
  for (const scene of args.scenes) {
    const src = path.join(args.from, variant, `${scene}.facts.json`);
    const facts = JSON.parse(fs.readFileSync(src, "utf8"));
    if (!facts.capture?.forcedDsf) throw new Error(`${src} 没有 forcedDsf 标记（旧采集器的产物），请先重新采集`);
    delete facts.aria; // ARIA 文本由节点序列携带，入库不再重复
    const dst = path.join(args.to, variant, `${scene}.facts.json`);
    fs.mkdirSync(path.dirname(dst), { recursive: true });
    const text = `${JSON.stringify(facts)}\n`;
    fs.writeFileSync(dst, text);
    bytes += text.length;
    console.log(`${variant}/${scene}：${(text.length / 1024).toFixed(1)} KB`);
  }
}
console.log(`共 ${(bytes / 1024).toFixed(1)} KB → ${args.to}`);
