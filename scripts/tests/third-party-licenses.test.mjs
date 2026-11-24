import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

// 第三方许可证随包守卫：Viva（Apache-2.0）与 go-winio（MIT）再分发时必须附带许可证文本。
// 构建脚本是 PowerShell，这里对脚本文本做静态核对；真实打包由 build-shell / build-installer 在发版时执行。
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const thirdParty = path.join(root, "third-party");
const buildShell = fs.readFileSync(path.join(root, "apps/desktop-go/scripts/build-shell.ps1"), "utf8");
const buildInstaller = fs.readFileSync(path.join(root, "apps/desktop-go/scripts/build-installer.ps1"), "utf8");

// 随包许可证的体积上限：当前合计约 28 KB；超过 64 KiB 说明有人把非许可证内容放进了 third-party。
const LICENSE_BUNDLE_MAX_BYTES = 64 * 1024;

function listFiles(dir, base = dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? listFiles(full, base) : [path.relative(base, full).split(path.sep).join("/")];
  });
}

test("third-party 下的许可证文件齐全且内容是对应的许可证", () => {
  const read = (rel) => fs.readFileSync(path.join(thirdParty, rel), "utf8");
  assert.match(read("viva/LICENSE"), /Apache License\s+Version 2\.0/);
  assert.match(read("viva/NOTICE"), /Viva/);
  assert.match(read("viva/THIRD_PARTY_NOTICES.md"), /Third-party notices/);
  assert.match(read("go-winio/LICENSE"), /MIT License/);
});

test("build-shell.ps1 把 third-party 下每个文件都复制进发布目录，新增文件不会被漏掉", () => {
  const files = listFiles(thirdParty);
  assert.ok(files.length >= 4, `third-party 文件数异常：${files.join(", ")}`);
  const listed = buildShell.match(/\$licenseFiles\s*=\s*@\(([^)]*)\)/);
  assert.ok(listed, "build-shell.ps1 缺少 $licenseFiles 清单");
  const copied = [...listed[1].matchAll(/"([^"]+)"/g)].map((m) => m[1].replace(/\\/g, "/"));
  assert.deepEqual([...copied].sort(), [...files].sort(), "build-shell.ps1 的随包清单与 third-party 目录不一致");
  assert.match(buildShell, /third-party\\\$rel/, "必须复制到发布目录的 third-party 下");
});

test("build-installer.ps1 缺 Viva 或 go-winio 的 LICENSE 时拒绝打包", () => {
  for (const rel of ["third-party\\viva\\LICENSE", "third-party\\go-winio\\LICENSE"]) {
    assert.ok(buildInstaller.includes(`"${rel}"`), `build-installer.ps1 没有检查 ${rel}`);
  }
  assert.match(buildInstaller, /Test-Path \(Join-Path \$shellDir \$rel\)\)\)\s*\{\s*throw/);
});

test("随包许可证的总体积在预算内", () => {
  const total = listFiles(thirdParty).reduce((sum, rel) => sum + fs.statSync(path.join(thirdParty, rel)).size, 0);
  assert.ok(total > 0 && total <= LICENSE_BUNDLE_MAX_BYTES, `third-party 合计 ${total} 字节，超过上限 ${LICENSE_BUNDLE_MAX_BYTES}`);
});
