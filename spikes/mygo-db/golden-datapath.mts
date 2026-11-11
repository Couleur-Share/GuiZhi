// 数据目录解析/检查与 shell:openPath 的差分基准：真实的 data-path.ts 与 shell-open-path.ts，在临时目录树里运行。
import fs from "node:fs";
import path from "node:path";

export interface DataPathGoldenCtx {
  out: string;
}

/** 目录树：路径 → 文件内容（string）或目录（null）。 */
type Tree = Record<string, string | null>;

const ROOT = "<ROOT>";

function build(root: string, tree: Tree): void {
  fs.rmSync(root, { recursive: true, force: true });
  fs.mkdirSync(root, { recursive: true });
  for (const [rel, content] of Object.entries(tree)) {
    const full = path.join(root, rel);
    if (content === null) fs.mkdirSync(full, { recursive: true });
    else {
      fs.mkdirSync(path.dirname(full), { recursive: true });
      fs.writeFileSync(full, content, "utf8");
    }
  }
}

export async function buildDataPathGolden(ctx: DataPathGoldenCtx): Promise<unknown> {
  const dp = await import("../../apps/desktop/src/main/data-path.ts");
  const sp = await import("../../apps/desktop/src/main/shell-open-path.ts");
  const base = path.join(ctx.out, "dp");
  fs.rmSync(base, { recursive: true, force: true });
  fs.mkdirSync(base, { recursive: true });
  const norm = (s: string, root: string) => s.split(root).join(ROOT).split(root.replace(/\\/g, "/")).join(ROOT).replace(/\\/g, "/");
  const normDeep = (v: unknown, root: string): unknown => JSON.parse(JSON.stringify(v ?? null, (_k, x) => (typeof x === "string" ? norm(x, root) : x)));

  // ── inspect / isLinkSafeDataPathRoot ──
  const FULL: Tree = { "data": null, "data/knowledge.db": "x", "config": null, "backups": null, "logs": null, "Local Storage": null, "Session Storage": null, "shortcuts.json": "{}", "shortcut-mode.json": "{}" };
  const inspectCases: Array<{ name: string; tree: Tree; target: string | "<empty>" }> = [
    { name: "missing", tree: {}, target: "nope" },
    { name: "emptyDir", tree: { "t": null }, target: "t" },
    { name: "full", tree: Object.fromEntries(Object.entries(FULL).map(([k, v]) => ["t/" + k, v])), target: "t" },
    { name: "dataNoDb", tree: { "t/data": null }, target: "t" },
    { name: "dbIsDir", tree: { "t/data/knowledge.db": null }, target: "t" },
    { name: "dataIsFile", tree: { "t/data": "not a dir" }, target: "t" },
    { name: "fileTarget", tree: { "t": "I am a file" }, target: "t" },
    { name: "markerKinds", tree: { "t/config": "file", "t/logs": null, "t/shortcuts.json": null, "t/other.txt": "x" }, target: "t" },
    { name: "emptyString", tree: {}, target: "<empty>" },
    { name: "trailingSlash", tree: { "t/config": null }, target: "t/" },
    { name: "dotSegments", tree: { "t/sub": null, "t/config": null }, target: "t/sub/../" },
  ];
  const inspect: unknown[] = [];
  for (const c of inspectCases) {
    const root = path.join(base, "inspect-" + c.name);
    build(root, c.tree);
    const target = c.target === "<empty>" ? "" : path.join(root, c.target) + (c.target.endsWith("/") ? path.sep : "");
    inspect.push({
      name: c.name, tree: c.tree, target: c.target,
      inspection: normDeep(dp.inspectDataPath(target), root),
      linkSafe: dp.isLinkSafeDataPathRoot(target === "" ? root : target),
      hasExisting: dp.hasExistingAppData(target),
    });
  }

  // ── readConfiguredDataPath / writeConfiguredDataPath ──
  const cfgCases: Array<{ name: string; file: string | null }> = [
    { name: "none", file: null },
    { name: "badJson", file: "{oops" },
    { name: "notObject", file: "[1,2]" },
    { name: "nullRoot", file: "null" },
    { name: "numberPath", file: JSON.stringify({ dataPath: 5 }) },
    { name: "blank", file: JSON.stringify({ dataPath: "   " }) },
    { name: "empty", file: JSON.stringify({ dataPath: "" }) },
    { name: "valid", file: JSON.stringify({ dataPath: `${ROOT}/custom/data`, updatedAt: "x" }) },
    { name: "validTrailing", file: JSON.stringify({ dataPath: `${ROOT}/custom/data/` }) },
    { name: "validDots", file: JSON.stringify({ dataPath: `${ROOT}/a/../b` }) },
    { name: "validForwardSlashDrive", file: JSON.stringify({ dataPath: "{ROOTFWD}/z" }) },
  ];
  const readCfg: unknown[] = [];
  for (const c of cfgCases) {
    const root = path.join(base, "cfg-" + c.name);
    fs.rmSync(root, { recursive: true, force: true });
    fs.mkdirSync(root, { recursive: true });
    let text = c.file;
    if (text !== null) {
      text = text.split(ROOT).join(root.replace(/\\/g, "/")).split("{ROOTFWD}").join(root.replace(/\\/g, "/"));
      fs.mkdirSync(path.join(root, "GuiZhi"), { recursive: true });
      fs.writeFileSync(path.join(root, "GuiZhi", "data-path.json"), text, "utf8");
    }
    const got = dp.readConfiguredDataPath(root);
    readCfg.push({ name: c.name, file: c.file, value: got === null ? null : norm(got, root) });
  }
  const writeRoot = path.join(base, "write");
  fs.mkdirSync(writeRoot, { recursive: true });
  dp.writeConfiguredDataPath(writeRoot, path.join(writeRoot, "x", "..", "dest"));
  const written = fs.readFileSync(path.join(writeRoot, "GuiZhi", "data-path.json"), "utf8");
  const writeResult = written.replace(/"updatedAt": "[^"]*"/, '"updatedAt": "NOW"').split(JSON.stringify(writeRoot).slice(1, -1)).join(ROOT).replace(/\\\\/g, "/");

  // ── resolveInitialUserDataPath（真实文件系统依赖） ──
  interface ResolveCase { name: string; appData: Tree; exe: string; packaged: boolean; platform: string }
  const R = (rel: string) => `${ROOT}/${rel}`;
  const dataTree = (prefix: string): Tree => Object.fromEntries(Object.entries({ "data": null, "data/knowledge.db": "x" }).map(([k, v]) => [prefix + k, v]));
  const resolveCases: ResolveCase[] = [
    { name: "configured", appData: { "appdata/GuiZhi/data-path.json": JSON.stringify({ dataPath: R("chosen") }), ...dataTree("appdata/GuiZhi/") }, exe: R("inst/GuiZhi.exe"), packaged: true, platform: "win32" },
    { name: "defaultHasData", appData: dataTree("appdata/GuiZhi/"), exe: R("inst/GuiZhi.exe"), packaged: true, platform: "win32" },
    { name: "installScopedHasData", appData: { "appdata": null, ...dataTree("inst/data/") }, exe: R("inst/GuiZhi.exe"), packaged: true, platform: "win32" },
    { name: "installScopedEmpty", appData: { "appdata": null, "inst/data": null }, exe: R("inst/GuiZhi.exe"), packaged: true, platform: "win32" },
    { name: "installScopedNotPackaged", appData: { "appdata": null, ...dataTree("inst/data/") }, exe: R("inst/GuiZhi.exe"), packaged: false, platform: "win32" },
    { name: "installDirMissing", appData: { "appdata": null }, exe: R("nowhere/GuiZhi.exe"), packaged: true, platform: "win32" },
    { name: "protectedProgramFiles", appData: { "appdata": null }, exe: "C:\\Program Files\\GuiZhi\\GuiZhi.exe", packaged: true, platform: "win32" },
    { name: "perUserDefaultInstall", appData: { "appdata": null }, exe: "C:\\Users\\u\\AppData\\Local\\Programs\\GuiZhi\\GuiZhi.exe", packaged: true, platform: "win32" },
    { name: "forwardSlashExe", appData: { "appdata": null, ...dataTree("inst/data/") }, exe: R("inst/GuiZhi.exe").replace(/\\/g, "/"), packaged: true, platform: "win32" },
    { name: "darwinIgnoresInstall", appData: { "appdata": null, ...dataTree("inst/data/") }, exe: R("inst/GuiZhi.exe"), packaged: true, platform: "darwin" },
    { name: "configuredBlank", appData: { "appdata/GuiZhi/data-path.json": JSON.stringify({ dataPath: " " }), ...dataTree("appdata/GuiZhi/") }, exe: R("inst/GuiZhi.exe"), packaged: true, platform: "win32" },
  ];
  const resolveOut: unknown[] = [];
  for (const c of resolveCases) {
    const root = path.join(base, "resolve-" + c.name);
    const subst = (s: string) => s.split(ROOT).join(root.replace(/\\/g, "/"));
    const tree: Tree = {};
    for (const [k, v] of Object.entries(c.appData)) tree[k] = typeof v === "string" ? subst(v) : v;
    build(root, tree);
    const appDataPath = path.join(root, "appdata");
    fs.mkdirSync(appDataPath, { recursive: true });
    const exe = subst(c.exe);
    const value = dp.resolveInitialUserDataPath({
      appDataPath,
      defaultUserDataPath: dp.getHistoricalDefaultUserDataPath(appDataPath, c.platform as NodeJS.Platform),
      exePath: exe,
      isPackaged: c.packaged,
      platform: c.platform as NodeJS.Platform,
    });
    resolveOut.push({ name: c.name, tree: c.appData, exe: c.exe, packaged: c.packaged, platform: c.platform, value: norm(value, root) });
  }

  // ── shell:openPath（假依赖，记录调用） ──
  interface OpenCase { name: string; tree: Tree; arg: unknown; kind?: "symlink" }
  const openCases: OpenCase[] = [
    { name: "dir", tree: { "d": null }, arg: `${ROOT}/d` },
    { name: "file", tree: { "f.txt": "x" }, arg: `${ROOT}/f.txt` },
    { name: "missing", tree: {}, arg: `${ROOT}/missing` },
    { name: "empty", tree: {}, arg: "" },
    { name: "blank", tree: {}, arg: "   " },
    { name: "notString", tree: {}, arg: 5 },
    { name: "null", tree: {}, arg: null },
    { name: "tilde", tree: { "home/sub": null }, arg: "~/sub" },
    { name: "tildeAlone", tree: { "home": null }, arg: "~" },
    { name: "tildeBackslash", tree: { "home/sub": null }, arg: "~\\sub" },
    { name: "tildeUser", tree: {}, arg: "~user/x" },
    { name: "appdataVar", tree: { "appdata/GuiZhi": null }, arg: "%APPDATA%\\GuiZhi" },
    { name: "appdataVarLower", tree: { "appdata/GuiZhi": null }, arg: "%appdata%/GuiZhi" },
    { name: "localAppdataVar", tree: { "local/GuiZhi": null }, arg: "%LOCALAPPDATA%\\GuiZhi" },
    { name: "midTilde", tree: {}, arg: `${ROOT}/a~b` },
    { name: "openPathError", tree: { "err": null }, arg: `${ROOT}/err` },
  ];
  const openOut: unknown[] = [];
  for (const c of openCases) {
    const root = path.join(base, "open-" + c.name);
    build(root, c.tree);
    const rootFwd = root.replace(/\\/g, "/");
    const arg = typeof c.arg === "string" ? c.arg.split(ROOT).join(rootFwd) : c.arg;
    const calls: unknown[] = [];
    const deps = {
      appDataPath: path.join(root, "appdata"),
      homePath: path.join(root, "home"),
      localAppDataPath: path.join(root, "local"),
      lstatSync: fs.lstatSync,
      statSync: fs.statSync,
      openPath: async (p: string) => { calls.push({ openPath: norm(p, root) }); return c.name === "openPathError" ? "boom: cannot open" : ""; },
      showItemInFolder: (p: string) => { calls.push({ showItemInFolder: norm(p, root) }); },
    };
    const result = await sp.openDirectoryPath(arg as string, deps as never);
    openOut.push({ name: c.name, tree: c.tree, arg: c.arg, result: normDeep(result, root), calls });
  }

  return { inspect, readCfg, writeResult, resolve: resolveOut, open: openOut };
}
