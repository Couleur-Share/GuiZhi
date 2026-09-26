import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import test from "node:test";
import hooks from "../../apps/desktop/scripts/crawl4ai-package.cjs";

function fixture(t) {
  const resources = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-package-test-"));
  t.after(() => fs.rmSync(resources, { recursive: true, force: true }));
  const runtime = path.join(resources, "crawl4ai");
  const worker = path.join(resources, "crawl4ai-worker");
  fs.mkdirSync(path.join(runtime, "python"), { recursive: true });
  fs.mkdirSync(worker);
  fs.writeFileSync(path.join(runtime, "python/python.exe"), "Python");
  fs.writeFileSync(path.join(worker, "extract-only.py"), "提取");
  const manifest = path.join(runtime, "manifest.json");
  fs.writeFileSync(manifest, JSON.stringify({ protocol: 1, version: "0.9.3",
    target: "win32-x64", python: "python/python.exe", browser: "browser/chrome.exe",
    files: { "browser/chrome.exe": "旧哈希" } }));
  return { resources, runtime, manifest };
}

test("分发清单移除旧浏览器字段，并在签名改变文件后重算哈希", (t) => {
  const { resources, runtime, manifest } = fixture(t);
  hooks.finalizeManifest(resources);
  let value = JSON.parse(fs.readFileSync(manifest, "utf8"));
  assert.equal(value.renderer, "electron");
  assert.equal("browser" in value, false);
  assert.deepEqual(Object.keys(value.files), ["python/python.exe"]);
  assert.equal(value.workerHashes["extract-only.py"], createHash("sha256").update("提取").digest("hex"));
  fs.appendFileSync(path.join(runtime, "python/python.exe"), "签名");
  hooks.finalizeManifest(resources);
  const signed = JSON.parse(fs.readFileSync(manifest, "utf8"));
  assert.notEqual(signed.files["python/python.exe"], value.files["python/python.exe"]);
});

test("资源过滤失效时拒绝打包，不能悄悄重登记 Chromium", (t) => {
  const { resources, runtime, manifest } = fixture(t);
  const before = fs.readFileSync(manifest, "utf8");
  fs.mkdirSync(path.join(runtime, "browser"));
  fs.writeFileSync(path.join(runtime, "browser/chrome.exe"), "Chrome");
  assert.throws(() => hooks.finalizeManifest(resources), /独立 Chromium/);
  assert.equal(fs.readFileSync(manifest, "utf8"), before);
});

test("Python 缺失时不能生成看似有效的分发清单", (t) => {
  const { resources, runtime } = fixture(t);
  fs.unlinkSync(path.join(runtime, "python/python.exe"));
  assert.throws(() => hooks.finalizeManifest(resources), /缺少 Python/);
});
