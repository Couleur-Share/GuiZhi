import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createDesktopSnapshot } from "../desktop-validation-snapshot.mjs";
import {
  goShotArgs, goValidationEnv, huePresetIds, nativeAppearanceFlags, nativeMatrixVariants, nativeShotArgs, nativeShotEnv, omitEnv,
} from "../isolated-go.mjs";

test("omitEnv 去掉指定变量（不区分大小写），不改动传入对象", () => {
  const env = { PATH: "tools", PSModulePath: "C:/pwsh7/Modules", other: "x" };
  assert.deepEqual(omitEnv(env, "PSMODULEPATH"), { PATH: "tools", other: "x" });
  assert.deepEqual(omitEnv(env, "psmodulepath"), { PATH: "tools", other: "x" });
  assert.deepEqual(omitEnv(env, "NOPE"), env);
  assert.equal(env.PSModulePath, "C:/pwsh7/Modules");
});

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

test("nativeShotArgs：缺省值、路径按调用方目录解析、非法取值给出可操作的错误", () => {
  const cwd = path.resolve("some", "dir");
  const defaults = nativeShotArgs([], cwd);
  assert.equal(defaults.module, "imports");
  assert.equal(defaults.theme, "light");
  assert.equal(defaults.lang, "zh-Hans");
  assert.equal(defaults.name, "native-imports-light-zh-Hans", "文件名默认带上页面、主题与语言，多张截图互不覆盖");
  assert.equal(path.relative(path.resolve(import.meta.dirname, "../.."), defaults.out).replaceAll("\\", "/"), "apps/desktop-go/.tmp-shots");

  const custom = nativeShotArgs(
    ["--module", "wiki", "--theme", "dark", "--lang", "en", "--width", "1024", "--height", "640", "--wait", "500ms", "--out", "o", "--data-db", "x.db", "--name", "mine"],
    cwd,
  );
  assert.deepEqual(
    { ...custom, out: path.relative(cwd, custom.out), dataDb: path.relative(cwd, custom.dataDb) },
    {
      module: "wiki", theme: "dark", lang: "en", width: 1024, height: 640, wait: "500ms", out: "o", name: "mine", dataDb: "x.db",
      hue: null, font: null, motion: null, wallpaper: null, wallpaperOpacity: null, wallpaperBlur: null, matrix: null,
    },
  );

  assert.throws(() => nativeShotArgs(["--module", "nope"], cwd), /--module 取值/);
  assert.throws(() => nativeShotArgs(["--theme", "blue"], cwd), /--theme 取值/);
  assert.throws(() => nativeShotArgs(["--lang", "fr"], cwd), /--lang 取值/);
  assert.throws(() => nativeShotArgs(["--wait", "soon"], cwd), /--wait 需要/);
  assert.throws(() => nativeShotArgs(["--width", "10"], cwd), /--width 需要/);
  assert.throws(() => nativeShotArgs(["--width"], cwd), /--width 缺少值/);
  assert.throws(() => nativeShotArgs(["--steps", "a.mjs"], cwd), /未知参数 --steps/);
});

test("nativeShotArgs：外观开关只在给出时透传，文件名带上偏离默认的外观项，非法取值在构建前就报错", () => {
  const cwd = path.resolve("some", "dir");
  const a = nativeShotArgs(
    ["--theme", "system", "--hue", "150,30", "--font", "large", "--motion", "off", "--wallpaper", "demo", "--wallpaper-opacity", "0.6", "--wallpaper-blur", "12"],
    cwd,
  );
  assert.equal(a.name, "native-imports-system-zh-Hans-hue-150x30-font-large-motion-off-wallpaper-blur12");
  assert.deepEqual(nativeAppearanceFlags(a), [
    "-native-hue", "150,30", "-native-font", "large", "-native-motion", "off",
    "-native-wallpaper", "demo", "-native-wallpaper-opacity", "0.6", "-native-wallpaper-blur", "12",
  ]);
  assert.deepEqual(nativeAppearanceFlags(nativeShotArgs([], cwd)), [], "没给外观开关就不传，由 Go 侧默认值决定");
  assert.equal(nativeShotArgs(["--hue", "teal"], cwd).name, "native-imports-light-zh-Hans-hue-teal");

  assert.throws(() => nativeShotArgs(["--hue", "Teal Blue!"], cwd), /--hue 需要/);
  assert.throws(() => nativeShotArgs(["--font", "huge"], cwd), /--font 取值/);
  assert.throws(() => nativeShotArgs(["--motion", "fast"], cwd), /--motion 取值/);
  assert.throws(() => nativeShotArgs(["--wallpaper-blur", "12"], cwd), /需要同时给出 --wallpaper/);
  assert.throws(() => nativeShotArgs(["--wallpaper", "demo", "--wallpaper-blur", "99"], cwd), /--wallpaper-blur 需要 0–50/);
  assert.throws(() => nativeShotArgs(["--wallpaper", "demo", "--wallpaper-opacity", ""], cwd), /--wallpaper-opacity 需要/);
  assert.throws(() => nativeShotArgs(["--wallpaper", "no-such-file.png"], cwd), /文件不存在/);
});

test("nativeMatrixVariants：矩阵展开成完整参数，每格独立命名；没有矩阵就是原参数一格", () => {
  const cwd = path.resolve("some", "dir");
  const presets = ["royal-blue", "blue", "purple", "green", "orange", "teal"];

  const single = nativeShotArgs(["--module", "wiki"], cwd);
  assert.deepEqual(nativeMatrixVariants(single, presets), [single]);

  const appearance = nativeMatrixVariants(nativeShotArgs(["--matrix", "appearance"], cwd), presets);
  assert.equal(appearance.length, 12, "6 个预设 × 浅/深");
  assert.equal(new Set(appearance.map((v) => v.name)).size, 12, "每格文件名唯一");
  assert.ok(appearance.every((v) => v.matrix === null), "展开后的格不再带矩阵");
  assert.deepEqual(
    appearance.slice(0, 2).map((v) => [v.hue, v.theme, v.name]),
    [["royal-blue", "light", "native-imports-light-zh-Hans-hue-royal-blue"], ["royal-blue", "dark", "native-imports-dark-zh-Hans-hue-royal-blue"]],
  );

  const fonts = nativeMatrixVariants(nativeShotArgs(["--matrix", "fonts", "--theme", "dark"], cwd), presets);
  assert.deepEqual(fonts.map((v) => [v.font, v.theme]), [["small", "dark"], ["medium", "dark"], ["large", "dark"]], "字号矩阵沿用 --theme");

  const glass = nativeMatrixVariants(nativeShotArgs(["--matrix", "glass"], cwd), presets);
  assert.deepEqual(
    glass.map((v) => v.name),
    [
      "native-imports-light-zh-Hans-wallpaper", "native-imports-light-zh-Hans-wallpaper-blur12",
      "native-imports-dark-zh-Hans-wallpaper", "native-imports-dark-zh-Hans-wallpaper-blur12",
    ],
  );
  assert.ok(glass.every((v) => v.wallpaper === "demo"));

  assert.throws(() => nativeShotArgs(["--matrix", "nope"], cwd), /--matrix 取值/);
  assert.throws(() => nativeShotArgs(["--matrix", "fonts", "--name", "x"], cwd), /不能同时指定 --name/);
});

test("huePresetIds 直接读渲染层设置源文件里的预设，当前是 6 个", () => {
  const source = fs.readFileSync(path.resolve(import.meta.dirname, "../../apps/desktop/src/renderer/stores/settings/settings-appearance.ts"), "utf8");
  assert.deepEqual(huePresetIds(source), ["royal-blue", "blue", "purple", "green", "orange", "teal"]);
  assert.throws(() => huePresetIds("export const x = 1;"), /找不到 MORANDI_THEMES/);
});

test("nativeShotEnv 把 APPDATA 指到一次性目录并用 MYGO_LOCALE 指定语言，且不继承 WebView2 变量", () => {
  const env = { PATH: "tools", APPDATA: "C:/Users/me/AppData/Roaming", WEBVIEW2_USER_DATA_FOLDER: "C:/real/WebView2" };
  const isolated = nativeShotEnv(env, path.resolve("tmp", "run"), "en");
  assert.equal(isolated.APPDATA, path.join(path.resolve("tmp", "run"), "appdata"));
  assert.equal(isolated.MYGO_LOCALE, "en");
  assert.equal(isolated.WEBVIEW2_USER_DATA_FOLDER, undefined);
  assert.equal(isolated.PATH, "tools");
  assert.equal(env.APPDATA, "C:/Users/me/AppData/Roaming", "不能改动传入的对象");
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
