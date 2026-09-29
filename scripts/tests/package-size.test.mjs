import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import {
  DEFAULT_BUDGET_FILE,
  checkInstaller,
  checkRuntime,
  measureRuntime,
  run,
} from "../check-package-size.mjs";

const script = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", "check-package-size.mjs");
const MIB = 1024 * 1024;

function workspace(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-size-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return root;
}

function write(file, size) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, Buffer.alloc(size, 1));
}

function budgetFile(root, runtime = { maxBytes: 1000, maxFiles: 5 }, installer = { maxBytes: 5000 }) {
  const file = path.join(root, "budget.json");
  fs.writeFileSync(file, JSON.stringify({ runtime, installer }));
  return file;
}

function capture() {
  const lines = { log: [], error: [] };
  return { lines, log: { log: (m) => lines.log.push(m), error: (m) => lines.error.push(m) } };
}

test("统计运行包时排除顶层 browser/，与 electron-builder 资源过滤一致；嵌套同名目录仍计入", (t) => {
  const root = workspace(t);
  write(path.join(root, "python/python.exe"), 100);
  write(path.join(root, "site-packages/pkg/a.py"), 10);
  write(path.join(root, "site-packages/pkg/browser/keep.txt"), 5);
  write(path.join(root, "browser/chrome.exe"), 100000);
  assert.deepEqual(measureRuntime(root), { bytes: 115, files: 3 });
});

test("运行包：字节数与文件数分别判定，恰好等于上限时通过", () => {
  const budget = { maxBytes: 1000, maxFiles: 5 };
  assert.deepEqual(checkRuntime({ bytes: 1000, files: 5 }, budget), []);
  assert.match(checkRuntime({ bytes: 1001, files: 5 }, budget).join(), /运行包 .* MiB，超过上限/);
  assert.match(checkRuntime({ bytes: 10, files: 6 }, budget).join(), /6 个文件，超过上限 5 个/);
  assert.equal(checkRuntime({ bytes: 2000, files: 9 }, budget).length, 2);
});

test("安装包：超过上限时给出超出量", () => {
  assert.deepEqual(checkInstaller(5000, { maxBytes: 5000 }), []);
  const [problem] = checkInstaller(130 * MIB, { maxBytes: 120 * MIB });
  assert.match(problem, /安装包 130\.0 MiB，超过上限 120\.0 MiB（超出 10\.0 MiB）/);
});

test("命令行：预算内返回 0，超出返回 1 并提示如何处理，产物缺失或参数错误返回 2", (t) => {
  const root = workspace(t);
  const runtime = path.join(root, "runtime");
  write(path.join(runtime, "python/python.exe"), 100);
  const installer = path.join(root, "setup.exe");
  write(installer, 4000);
  const budget = budgetFile(root);

  const ok = capture();
  assert.equal(run(["--runtime", runtime, "--installer", installer, "--budget", budget], ok.log), 0);
  assert.match(ok.lines.log.join("\n"), /体积门禁通过/);

  write(path.join(runtime, "site-packages/big.bin"), 2000);
  const over = capture();
  assert.equal(run(["--runtime", runtime, "--budget", budget], over.log), 1);
  assert.match(over.lines.error.join("\n"), /体积门禁失败：运行包/);
  assert.match(over.lines.error.join("\n"), /config\/package-size-budget\.json/);

  const missing = capture();
  assert.equal(run(["--installer", path.join(root, "missing.exe"), "--budget", budget], missing.log), 2);
  assert.match(missing.lines.error.join("\n"), /无法读取待检查产物/);

  for (const argv of [[], ["--runtime"], ["--unknown", "x"]]) {
    const usage = capture();
    assert.equal(run(argv, usage.log), 2);
    assert.match(usage.lines.error.join("\n"), /用法/);
  }
});

test("预算文件缺字段或数值无效时拒绝运行，不能把配置错误当成通过", (t) => {
  const root = workspace(t);
  const runtime = path.join(root, "runtime");
  write(path.join(runtime, "a.bin"), 1);
  for (const broken of [
    { runtime: { maxBytes: 1000 }, installer: { maxBytes: 5000 } },
    { runtime: { maxBytes: 0, maxFiles: 5 }, installer: { maxBytes: 5000 } },
    { runtime: { maxBytes: 1000, maxFiles: 5 }, installer: {} },
  ]) {
    const file = path.join(root, "broken.json");
    fs.writeFileSync(file, JSON.stringify(broken));
    const output = capture();
    assert.equal(run(["--runtime", runtime, "--budget", file], output.log), 2);
    assert.match(output.lines.error.join("\n"), /预算文件缺少有效的/);
  }
});

test("作为脚本执行时用退出码把结果交给 CI", (t) => {
  const root = workspace(t);
  const runtime = path.join(root, "runtime");
  write(path.join(runtime, "a.bin"), 10);
  const pass = spawnSync(process.execPath, [script, "--runtime", runtime, "--budget", budgetFile(root)], { encoding: "utf8" });
  assert.equal(pass.status, 0, pass.stderr);
  const fail = spawnSync(process.execPath, [script, "--runtime", runtime, "--budget", budgetFile(root, { maxBytes: 5, maxFiles: 5 })], { encoding: "utf8" });
  assert.equal(fail.status, 1);
  assert.match(fail.stderr, /体积门禁失败/);
});

test("仓库里的预算文件可解析，且发布用的上限是有限的正整数", () => {
  const budget = JSON.parse(fs.readFileSync(DEFAULT_BUDGET_FILE, "utf8"));
  for (const value of [budget.runtime.maxBytes, budget.runtime.maxFiles, budget.installer.maxBytes])
    assert.ok(Number.isInteger(value) && value > 0, "预算必须是正整数");
});
