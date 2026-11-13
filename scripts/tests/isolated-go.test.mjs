import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createDesktopSnapshot } from "../desktop-validation-snapshot.mjs";
import { goShotArgs, goValidationEnv } from "../isolated-go.mjs";

test("goValidationEnv 不继承开发服务地址、Electron 模式与 WebView2 配置，保留其余变量", () => {
  const env = {
    PATH: "tools",
    VITE_DEV_SERVER_URL: "http://127.0.0.1:5173",
    ELECTRON_RUN_AS_NODE: "1",
    WEBVIEW2_USER_DATA_FOLDER: "C:/real/WebView2",
    WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: "--remote-debugging-port=9222",
    WEBVIEW2_BROWSER_EXECUTABLE_FOLDER: "C:/other",
  };
  const isolated = goValidationEnv(env);
  assert.equal(isolated.PATH, "tools");
  assert.equal(isolated.NODE_ENV, "production");
  for (const key of Object.keys(env).filter((key) => key !== "PATH")) assert.equal(isolated[key], undefined, key);
  assert.equal(env.WEBVIEW2_USER_DATA_FOLDER, "C:/real/WebView2", "不能改动传入的对象");
});

test("goShotArgs 按调用方目录解析路径参数，缺省 --out 落到 apps/desktop-go/.tmp-shots", () => {
  const cwd = path.resolve("some", "dir");
  const args = goShotArgs(["--steps", "a.mjs", "--data-db", "x.db", "--keep-profile"], cwd);
  assert.deepEqual(args.slice(0, 5), ["--steps", path.join(cwd, "a.mjs"), "--data-db", path.join(cwd, "x.db"), "--keep-profile"]);
  assert.equal(args[5], "--out");
  assert.equal(path.relative(path.resolve(import.meta.dirname, "../.."), args[6]).replaceAll("\\", "/"), "apps/desktop-go/.tmp-shots");
  assert.deepEqual(goShotArgs(["--out", "o"], cwd), ["--out", path.join(cwd, "o")], "显式 --out 不再追加默认值");
  assert.throws(() => goShotArgs(["--steps"], cwd), /--steps 缺少值/);
});

test("快照默认不复制 apps/desktop-go 源码；extraSourcePaths 追加后复制，并遵守 exclude", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-go-snapshot-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const write = (relative, text) => {
    const target = path.join(root, relative);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, text);
  };
  execFileSync("git", ["init", "--quiet"], { cwd: root });
  write("package.json", "{}");
  write("apps/desktop/package.json", "{}");
  write("apps/desktop-go/main.go", "package main");
  write("apps/desktop-go/package.json", "{}");
  write("apps/desktop-go/testdata/golden/big.bin", "x");
  write("apps/desktop-go/verify/real.mts", "x");
  execFileSync("git", ["add", "."], { cwd: root, stdio: "ignore" });
  write("apps/desktop-go/untracked.go", "package main"); // 未提交的新文件也要进副本

  const plain = createDesktopSnapshot(root, { reuseDependencies: false });
  try {
    assert.equal(fs.existsSync(path.join(plain.root, "apps/desktop-go/main.go")), false, "默认范围不含 Go 源码");
    assert.equal(fs.existsSync(path.join(plain.root, "apps/desktop-go/package.json")), true, "但仍含各 app 的 package.json");
    // 副本根目录必须是展开后的实路径（Windows 8.3 短路径会让 Vite 的模块解析对不上，CI 上出现过）。
    assert.equal(plain.root, fs.realpathSync.native(plain.root));
  } finally {
    plain.cleanup();
  }

  const withGo = createDesktopSnapshot(root, {
    reuseDependencies: false,
    extraSourcePaths: ["apps/desktop-go", ":(exclude)apps/desktop-go/testdata", ":(exclude)apps/desktop-go/verify"],
  });
  try {
    const has = (relative) => fs.existsSync(path.join(withGo.root, relative));
    assert.equal(has("apps/desktop-go/main.go"), true);
    assert.equal(has("apps/desktop-go/untracked.go"), true);
    assert.equal(has("apps/desktop-go/testdata/golden/big.bin"), false);
    assert.equal(has("apps/desktop-go/verify/real.mts"), false);
  } finally {
    withGo.cleanup();
  }
});
