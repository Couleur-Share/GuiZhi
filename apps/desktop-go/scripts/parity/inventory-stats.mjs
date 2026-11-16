#!/usr/bin/env node
/**
 * 盘点清单统计与一致性检查（Viva 对等工程的进度分母）。
 *
 * 读取 docs/agents/viva-parity-inventory/*.md 的分片，按固定格式解析表格行，输出：
 *   - 每个分片、每个模块的基础场景数（屏幕 × 状态）、对话框/弹层数、交互条数、快捷键数、动效条数、保真风险条数；
 *   - 完整场景总数 = 基础场景数 × 展开矩阵（明暗 × 色相 × 字号 × 视口，见 viva-ui-development.md §2.1）。
 * 检查：ID 全局唯一、命名符合 `<模块>.<屏幕>[.<状态>]`，且模块在允许的集合内。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/inventory-stats.mjs            打印统计（Markdown）
 *   node apps/desktop-go/scripts/parity/inventory-stats.mjs --check    只检查，有问题时以非零状态退出
 *   node apps/desktop-go/scripts/parity/inventory-stats.mjs --json     打印 JSON
 *   node apps/desktop-go/scripts/parity/inventory-stats.mjs --write    把分母表写回 docs/agents/viva-parity-inventory.md 的 stats 区块
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { writeBlock } from "./lib/doc-block.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(here, "../../../..");
export const PARTS_DIR = path.join(REPO, "docs/agents/viva-parity-inventory");

/** 展开矩阵（viva-ui-development.md §2.1）：明暗 2 × 色相 6 × 字号 3 × 视口 3。 */
export const MATRIX = { modes: 2, hues: 6, fonts: 3, viewports: 3 };
export const MATRIX_FACTOR = MATRIX.modes * MATRIX.hues * MATRIX.fonts * MATRIX.viewports;

/** 允许出现在场景 ID 第一段的模块名（渲染层 components 下的目录名，外加共享的 ui 控件）。 */
export const MODULES = new Set([
  "app", "layout", "setup", "migration", "ui", "library", "settings",
  "imports", "inbox", "capture", "ask", "wiki", "themed-reading",
]);

const ID_RE = /^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+$/;

/** 取表格行的第一个单元格（去掉反引号与加粗标记）；非表格行返回 null。 */
function firstCell(line) {
  if (!line.startsWith("|")) return null;
  const cells = line.split("|");
  if (cells.length < 3) return null;
  return cells[1].trim().replace(/^[`*]+|[`*]+$/g, "");
}

/** 按「## n.」切出各节。 */
function sections(text) {
  const out = new Map();
  let current = null;
  for (const line of text.split(/\r?\n/)) {
    const m = /^##\s+(\d+)\./.exec(line);
    if (m) {
      current = Number(m[1]);
      out.set(current, []);
    } else if (current !== null) {
      out.get(current).push(line);
    }
  }
  return out;
}

/** 一行是否是 Markdown 表格的分隔行（| --- | :---: |）。 */
function isSeparator(line) {
  return /^\|(\s*:?-{3,}:?\s*\|)+\s*$/.test(line.trim());
}

/** 表格数据行：跳过表头（下一行是分隔行的那一行）与分隔行本身。 */
function tableRows(lines) {
  const rows = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const cell = firstCell(line);
    if (cell === null || cell === "" || isSeparator(line)) continue;
    if (i + 1 < lines.length && isSeparator(lines[i + 1])) continue; // 表头
    rows.push({ cell, line });
  }
  return rows;
}

/** 编号列表项（"1. xxx"），保真风险等节有的分片写成列表而不是表格。 */
function numberedItems(lines) {
  return lines.filter((l) => /^\d+\.\s+\S/.test(l)).length;
}

export function parsePart(name, text) {
  const secs = sections(text);
  const rows = (n) => tableRows(secs.get(n) ?? []);
  const idRows = (n) => rows(n).filter((r) => ID_RE.test(r.cell));
  return {
    name,
    // §1 屏幕与区域：场景根；§2 状态矩阵：每个状态一行（ID 形如 root.state，也可能是 root 本身）
    roots: idRows(1).map((r) => r.cell),
    states: idRows(2).map((r) => r.cell),
    dialogs: idRows(3).map((r) => r.cell),
    interactions: idRows(4).map((r) => r.cell),
    shortcuts: rows(5).length,
    motions: rows(6).length,
    risks: rows(9).length + numberedItems(secs.get(9) ?? []),
  };
}

export function loadParts() {
  if (!fs.existsSync(PARTS_DIR)) return [];
  return fs.readdirSync(PARTS_DIR)
    .filter((f) => f.endsWith(".md") && f !== "README.md")
    .sort()
    .map((f) => parsePart(f.replace(/\.md$/, ""), fs.readFileSync(path.join(PARTS_DIR, f), "utf8")));
}

/** 状态行的所有者：自身或某个前缀是 §1 的场景根或 §3 的对话框；找不到返回 null。 */
export function ownerOf(part, id) {
  const owners = [...part.roots, ...part.dialogs];
  return owners.find((o) => id === o || id.startsWith(o + ".")) ?? null;
}

/**
 * 一个分片的基础场景集合（去重）：§1 的场景根 ∪ 能找到所有者的 §2 状态行 ∪ §3 的对话框/弹层。
 * 弹层的默认形态（打开的菜单、Toast、模态框）本身就是要验收的场景。
 */
export function baseScenes(part) {
  return [...new Set([...part.roots, ...part.states.filter((id) => ownerOf(part, id) !== null), ...part.dialogs])];
}

export function check(parts) {
  const problems = [];
  const seen = new Map(); // 基础场景 ID -> 分片名
  for (const part of parts) {
    for (const id of baseScenes(part)) {
      const mod = id.split(".")[0];
      if (!MODULES.has(mod)) problems.push(`${part.name}: 场景 ID ${id} 的模块 ${mod} 不在允许集合内`);
      if (seen.has(id) && seen.get(id) !== part.name) problems.push(`场景 ID ${id} 同时出现在 ${seen.get(id)} 与 ${part.name}`);
      seen.set(id, part.name);
    }
    for (const id of part.states) {
      if (ownerOf(part, id) === null) problems.push(`${part.name}: 状态行 ${id} 找不到所属的场景根（§1）或对话框（§3）；控件行请不要把 ID 写在第一列`);
    }
    const dlgSeen = new Set();
    for (const id of part.dialogs) {
      if (dlgSeen.has(id)) problems.push(`${part.name}: 对话框 ID ${id} 重复`);
      dlgSeen.add(id);
      // 第二段是类型（dlg/menu/popover/toast/tooltip/dropdown/banner…）或所属区域，不强制固定词。
      if (!MODULES.has(id.split(".")[0])) problems.push(`${part.name}: 对话框 ID ${id} 的模块不在允许集合内`);
    }
    const intSeen = new Set();
    for (const id of part.interactions) {
      if (intSeen.has(id)) problems.push(`${part.name}: 交互 ID ${id} 重复`);
      intSeen.add(id);
      if (!MODULES.has(id.split(".")[0])) problems.push(`${part.name}: 交互 ID ${id} 的模块不在允许集合内`);
    }
  }
  return problems;
}

export function stats(parts) {
  const byModule = new Map();
  const bump = (mod, key, n = 1) => {
    const e = byModule.get(mod) ?? { scenes: 0, dialogs: 0, interactions: 0 };
    e[key] += n;
    byModule.set(mod, e);
  };
  for (const part of parts) {
    for (const id of baseScenes(part)) bump(id.split(".")[0], "scenes");
    for (const id of part.dialogs) bump(id.split(".")[0], "dialogs");
    for (const id of part.interactions) bump(id.split(".")[0], "interactions");
  }
  return { byModule, parts };
}

export function renderMarkdown(parts) {
  const s = stats(parts);
  const lines = [];
  lines.push("| 分片 | 场景根 | 基础场景（含状态） | 对话框/弹层 | 交互 | 快捷键 | 动效 | 保真风险 |");
  lines.push("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |");
  const total = { roots: 0, scenes: 0, dialogs: 0, interactions: 0, shortcuts: 0, motions: 0, risks: 0 };
  for (const p of parts) {
    const scenes = baseScenes(p).length;
    lines.push(`| ${p.name} | ${p.roots.length} | ${scenes} | ${p.dialogs.length} | ${p.interactions.length} | ${p.shortcuts} | ${p.motions} | ${p.risks} |`);
    total.roots += p.roots.length; total.scenes += scenes; total.dialogs += p.dialogs.length;
    total.interactions += p.interactions.length; total.shortcuts += p.shortcuts; total.motions += p.motions; total.risks += p.risks;
  }
  lines.push(`| **合计** | ${total.roots} | ${total.scenes} | ${total.dialogs} | ${total.interactions} | ${total.shortcuts} | ${total.motions} | ${total.risks} |`);
  lines.push("");
  lines.push(`完整场景数 = 基础场景 ${total.scenes} × 展开矩阵 ${MATRIX_FACTOR}（明暗 ${MATRIX.modes} × 色相 ${MATRIX.hues} × 字号 ${MATRIX.fonts} × 视口 ${MATRIX.viewports}）= ${total.scenes * MATRIX_FACTOR}。`);
  lines.push("");
  lines.push("| 模块 | 基础场景 | 对话框/弹层 | 交互 |");
  lines.push("| --- | ---: | ---: | ---: |");
  for (const [mod, e] of [...s.byModule].sort((a, b) => a[0].localeCompare(b[0]))) {
    lines.push(`| ${mod} | ${e.scenes} | ${e.dialogs} | ${e.interactions} |`);
  }
  return lines.join("\n");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const parts = loadParts();
  const problems = check(parts);
  const args = new Set(process.argv.slice(2));
  if (args.has("--json")) {
    console.log(JSON.stringify({ parts: parts.map((p) => ({ ...p, baseScenes: baseScenes(p).length })), problems }, null, 2));
  } else if (args.has("--write")) {
    console.log(writeBlock("stats", renderMarkdown(parts)) ? "已更新 stats 区块。" : "stats 区块已是最新。");
  } else if (!args.has("--check")) {
    console.log(renderMarkdown(parts));
  }
  if (problems.length) {
    console.error(`盘点清单有 ${problems.length} 个问题：`);
    for (const p of problems) console.error("  - " + p);
    process.exit(1);
  }
  if (args.has("--check")) console.log(`盘点清单检查通过：${parts.length} 个分片。`);
}
