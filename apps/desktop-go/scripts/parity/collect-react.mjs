#!/usr/bin/env node
/**
 * React 参照采集器：对一组场景 × 一组外观/视口变体，产出 PNG 与事实 JSON。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/collect-react.mjs --scenes settings.general [--tier T0] [--variant <key,...>]
 *        [--repeat 2] [--out <目录>] [--data-db <knowledge.db>]
 *
 *   --scenes   场景 ID，逗号分隔；缺省为全部已注册场景
 *   --tier     运行档位 T0|T1|T2|T3（§2.6），缺省 T0
 *   --variant  显式变体，如 light-royal-blue-medium-1280x800；给了就不看 --tier
 *   --repeat   每个场景在同一会话里采 N 次（采集稳定性检查用），文件名带 .r<i> 后缀（N>1 时）
 *   --out      输出目录，缺省 <基准目录>/parity/react
 *
 * 前提：已有 React 参照构建（node apps/desktop-go/scripts/parity/build-react-ref.mjs）。
 * 隔离：一次性 userData 与 WebView2 配置、库用空库或 --data-db 给的副本；只按 PID 结束自己拉起的进程。
 *
 * 产物：<out>/<变体键>/<场景>.png、.facts.json、.aria.yml（PNG 不入库）。
 */
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { readBuildRecord } from "./build-react-ref.mjs";
import { captureRegionPng, collectFacts } from "./lib/react-facts.mjs";
import { loadMatrix, parseVariantKey, tierVariants, variantKey } from "./lib/matrix.mjs";
import { parityOutDir } from "./lib/paths.mjs";
import { applyVariant, parkPointer, settle } from "./lib/react-state.mjs";
import { openReactSession } from "./lib/react-session.mjs";
import { getScene, sceneIds } from "./scenes/react-scenes.mjs";

function parseArgs(argv) {
  const args = { scenes: null, tier: "T0", variants: null, repeat: 1, out: path.join(parityOutDir(), "react"), dataDb: null };
  const value = (i, name) => {
    if (argv[i] === undefined) throw new Error(`${name} 缺少值`);
    return argv[i];
  };
  for (let i = 0; i < argv.length; i += 1) {
    switch (argv[i]) {
      case "--scenes": args.scenes = value(++i, "--scenes").split(",").filter(Boolean); break;
      case "--tier": args.tier = value(++i, "--tier"); break;
      case "--variant": args.variants = value(++i, "--variant").split(",").filter(Boolean); break;
      case "--repeat": args.repeat = Number(value(++i, "--repeat")); break;
      case "--out": args.out = path.resolve(value(++i, "--out")); break;
      case "--data-db": args.dataDb = path.resolve(value(++i, "--data-db")); break;
      default: throw new Error(`未知参数 ${argv[i]}`);
    }
  }
  if (!Number.isInteger(args.repeat) || args.repeat < 1) throw new Error("--repeat 需要正整数");
  return args;
}

/** 采集一个场景一次，写文件，返回摘要。 */
async function captureScene(session, sceneId, variant, outDir, suffix) {
  const scene = getScene(sceneId);
  const { page } = session;
  await scene.enter({ win: page, session });
  await settle(page);
  const hovering = await parkPointer(session);
  if (hovering !== 0) throw new Error(`场景 ${sceneId}：采集前仍有 ${hovering} 个元素处于悬停态，状态不干净`);
  const facts = await collectFacts(session, { regionSelector: scene.region ?? null, scene: sceneId, variant });
  const png = await captureRegionPng(session, facts.region);
  const base = path.join(outDir, `${sceneId}${suffix}`);
  fs.writeFileSync(`${base}.png`, png);
  fs.writeFileSync(`${base}.facts.json`, JSON.stringify(facts, null, 1) + "\n");
  fs.writeFileSync(`${base}.aria.yml`, facts.aria + "\n");
  return { scene: sceneId, nodes: facts.nodes.length, textRects: facts.textRects.length, region: facts.region, png: `${base}.png` };
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const matrix = loadMatrix();
  const variants = args.variants ? args.variants.map((k) => parseVariantKey(k, matrix)) : tierVariants(args.tier, matrix);
  const scenes = args.scenes ?? sceneIds();
  for (const id of scenes) getScene(id); // 先校验，免得拉起会话后才发现拼错
  const record = readBuildRecord();
  if (!record) throw new Error("没有 React 参照构建：先运行 node apps/desktop-go/scripts/parity/build-react-ref.mjs");

  const session = await openReactSession({ executable: record.exe, dist: record.dist, dataDb: args.dataDb });
  const started = Date.now();
  let count = 0;
  try {
    for (const variant of variants) {
      const key = variantKey(variant);
      const dir = path.join(args.out, key);
      fs.mkdirSync(dir, { recursive: true });
      const applied = await applyVariant(session, variant, matrix);
      console.log(`变体 ${key}：根字号 ${applied.rootFontPx}px，dark=${applied.dark}，视口 ${applied.viewport.join("x")}`);
      for (const sceneId of scenes) {
        for (let r = 1; r <= args.repeat; r += 1) {
          const t0 = Date.now();
          const summary = await captureScene(session, sceneId, variant, dir, args.repeat > 1 ? `.r${r}` : "");
          count += 1;
          console.log(`  ${sceneId}${args.repeat > 1 ? ` #${r}` : ""}：${summary.nodes} 个节点，${summary.textRects} 个文字行，区域 ${summary.region.width}x${summary.region.height}，${Date.now() - t0}ms`);
        }
      }
    }
  } finally {
    await session.close();
  }
  console.log(`完成：${count} 次采集，用时 ${((Date.now() - started) / 1000).toFixed(1)}s；输出 ${args.out}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    await main();
  } catch (error) {
    console.error(error?.stack ?? error);
    process.exitCode = 1;
  }
}
