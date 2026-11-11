// 配置迁移（config:export/read/apply）与快捷键（shortcuts:*）的差分基准：
// 用真实的 config-transfer.ipc.ts / services/config-transfer / shortcuts.ts 在隔离 userData 里跑一遍，记录返回值与落盘文件。
import fs from "node:fs";
import path from "node:path";

export interface ConfigGoldenCtx {
  ipcHandlers: Map<string, (...args: unknown[]) => unknown>;
  ipcOn: Map<string, (...args: unknown[]) => unknown>;
  openQueue: Array<{ canceled: boolean; filePaths: string[] }>;
  saveQueue: Array<{ canceled: boolean; filePath?: string }>;
  dialogCalls: Array<{ kind: "open" | "save"; options: Record<string, unknown> }>;
  userData: string; // 隔离的 userData 目录（getPath("userData")）
  out: string; // golden 输出目录
}

type Json = Record<string, unknown>;

const STAMP_RE = /\d{8}-\d{6}/g;

function rmrf(p: string): void {
  fs.rmSync(p, { recursive: true, force: true });
}

function dumpTree(dir: string): Record<string, string> {
  const tree: Record<string, string> = {};
  if (!fs.existsSync(dir)) return tree;
  const walk = (cur: string) => {
    for (const entry of fs.readdirSync(cur, { withFileTypes: true })) {
      const full = path.join(cur, entry.name);
      if (entry.isDirectory()) walk(full);
      else tree[path.relative(dir, full).split(path.sep).join("/")] = fs.readFileSync(full, "utf8");
    }
  };
  walk(dir);
  return tree;
}

/** 新建快照目录名（当前时间戳）统一成 NEW；种子里的旧快照用的是 2020 年的固定时间戳，原样保留。 */
function normalizeName(name: string): string {
  return name.replace(/pre-import-(\d{8}-\d{6})/g, (m, stamp: string) => (stamp >= "20250101-000000" ? "pre-import-NEW" : m));
}

function normalizeTree(tree: Record<string, string>): Record<string, string> {
  const next: Record<string, string> = {};
  for (const [name, text] of Object.entries(tree)) {
    next[normalizeName(name)] = text.replace(/"updatedAt": "[^"]*"/g, '"updatedAt": "NOW"');
  }
  return Object.fromEntries(Object.entries(next).sort(([a], [b]) => (a < b ? -1 : 1)));
}

export async function buildConfigGolden(ctx: ConfigGoldenCtx): Promise<{ shortcuts: unknown; configTransfer: unknown }> {
  const cfgDir = path.join(ctx.userData, "config");
  const legacyNames = ["shortcuts.json", "shortcut-mode.json"];

  const resetEnv = (config: Record<string, string> = {}, legacy: Record<string, string> = {}) => {
    rmrf(cfgDir);
    for (const n of legacyNames) rmrf(path.join(ctx.userData, n));
    if (Object.keys(config).length > 0) fs.mkdirSync(cfgDir, { recursive: true });
    for (const [name, text] of Object.entries(config)) {
      fs.mkdirSync(path.dirname(path.join(cfgDir, name)), { recursive: true });
      fs.writeFileSync(path.join(cfgDir, name), text, "utf8");
    }
    for (const [name, text] of Object.entries(legacy)) fs.writeFileSync(path.join(ctx.userData, name), text, "utf8");
  };
  const dumpEnv = () => {
    const legacy: Record<string, string> = {};
    for (const n of legacyNames) {
      const p = path.join(ctx.userData, n);
      if (fs.existsSync(p)) legacy[n] = fs.readFileSync(p, "utf8");
    }
    return { config: normalizeTree(dumpTree(cfgDir)), legacy };
  };
  const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

  // ── 快捷键 ──
  const shortcuts = await import("../../apps/desktop/src/main/shortcuts.ts");
  shortcuts.registerShortcutsIPC();
  const callH = async (ch: string, ...args: unknown[]) => {
    try {
      return { value: await (ctx.ipcHandlers.get(ch) as (...a: unknown[]) => unknown)({}, ...args) };
    } catch (e) {
      return { error: errText(e) };
    }
  };
  const callOn = (ch: string, ...args: unknown[]) => {
    try {
      (ctx.ipcOn.get(ch) as (...a: unknown[]) => unknown)({}, ...args);
      return { value: null };
    } catch (e) {
      return { error: errText(e) };
    }
  };

  type ScOp =
    | { op: "load" }
    | { op: "get" }
    | { op: "getMode" }
    | { op: "set"; arg: unknown }
    | { op: "setMode"; arg: unknown }
    | { op: "persist"; accelerators: Record<string, string>; modes: Record<string, string> };
  const custom = JSON.stringify({ showApp: "Alt+Shift+Q", newItem: "Ctrl+Alt+N", search: "", settings: "Alt+Shift+S" }, null, 2);
  const scCases: Array<{ name: string; config?: Record<string, string>; legacy?: Record<string, string>; ops: ScOp[] }> = [
    { name: "defaults", ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "config-file", config: { "shortcuts.json": custom, "shortcut-mode.json": JSON.stringify({ showApp: "local", newItem: "global" }) }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "legacy-dir-fallback", legacy: { "shortcuts.json": custom, "shortcut-mode.json": JSON.stringify({ search: "global" }) }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "config-wins-over-legacy", config: { "shortcuts.json": JSON.stringify({ showApp: "A" }) }, legacy: { "shortcuts.json": custom, "shortcut-mode.json": JSON.stringify({ search: "global" }) }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "newPrompt-migration", config: { "shortcuts.json": JSON.stringify({ newPrompt: "Alt+1", extra: "Alt+2" }), "shortcut-mode.json": JSON.stringify({ newPrompt: "global", search: "bogus" }) }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "newPrompt-and-newItem", config: { "shortcuts.json": JSON.stringify({ newPrompt: "Alt+1", newItem: "Alt+3" }), "shortcut-mode.json": JSON.stringify({ newPrompt: "global", newItem: "local" }) }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "invalid-json", config: { "shortcuts.json": "{oops", "shortcut-mode.json": "[1,2" }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "array-and-null-shapes", config: { "shortcuts.json": "[\"a\",\"b\"]", "shortcut-mode.json": "null" }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "scalar-shapes", config: { "shortcuts.json": "42", "shortcut-mode.json": "\"x\"" }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "non-string-values", config: { "shortcuts.json": JSON.stringify({ showApp: 5, search: null, settings: ["x"], newItem: { a: 1 } }), "shortcut-mode.json": JSON.stringify({ showApp: 1, newItem: "global", search: null }) }, ops: [{ op: "load" }, { op: "get" }, { op: "getMode" }] },
    { name: "mode-array", config: { "shortcut-mode.json": JSON.stringify(["global"]) }, ops: [{ op: "load" }, { op: "getMode" }] },
    { name: "set-then-get", ops: [{ op: "load" }, { op: "set", arg: { showApp: "Alt+Q", newItem: "", extra: "X" } }, { op: "get" }, { op: "getMode" }] },
    { name: "set-keeps-legacy-untouched", legacy: { "shortcuts.json": custom }, ops: [{ op: "load" }, { op: "set", arg: { showApp: "Alt+Z" } }, { op: "get" }] },
    { name: "setMode-normalizes", ops: [{ op: "load" }, { op: "setMode", arg: { showApp: "local", newItem: "global", search: "bad", unknown: "global" } }, { op: "getMode" }] },
    { name: "setMode-non-object", ops: [{ op: "load" }, { op: "setMode", arg: null }, { op: "getMode" }, { op: "setMode", arg: ["global"] }, { op: "getMode" }] },
    { name: "persist-imported", ops: [{ op: "load" }, { op: "persist", accelerators: { showApp: "Alt+P", search: "", bogus: "X", newPrompt: "Alt+N" }, modes: { showApp: "local", newItem: "global", search: "x" } }, { op: "get" }, { op: "getMode" }] },
    { name: "persist-newPrompt-and-newItem", ops: [{ op: "load" }, { op: "persist", accelerators: { newPrompt: "A", newItem: "B" }, modes: { newPrompt: "global", newItem: "local" } }, { op: "get" }, { op: "getMode" }] },
    { name: "persist-non-string-ignored", ops: [{ op: "load" }, { op: "persist", accelerators: { showApp: 5 as unknown as string, newItem: null as unknown as string }, modes: {} }, { op: "get" }] },
  ];
  const scenariosOut: unknown[] = [];
  for (const c of scCases) {
    const opsSnapshot = JSON.parse(JSON.stringify(c.ops));
    resetEnv(c.config, c.legacy);
    const results: unknown[] = [];
    for (const op of c.ops) {
      switch (op.op) {
        case "load":
          shortcuts.registerShortcuts({ skipGlobal: true });
          results.push(null);
          break;
        case "get":
          results.push(await callH("shortcuts:get"));
          break;
        case "getMode":
          results.push(await callH("shortcuts:getMode"));
          break;
        case "set":
          results.push(await callH("shortcuts:set", op.arg));
          break;
        case "setMode":
          results.push(callOn("shortcuts:setMode", op.arg));
          break;
        case "persist":
          try {
            shortcuts.persistImportedShortcuts(op.accelerators, op.modes as never);
            results.push({ value: null });
          } catch (e) {
            results.push({ error: errText(e) });
          }
          break;
      }
    }
    scenariosOut.push({ name: c.name, config: c.config ?? {}, legacy: c.legacy ?? {}, ops: opsSnapshot, results, after: dumpEnv() });
  }

  // ── 配置迁移 ──
  const { registerConfigTransferIPC } = await import("../../apps/desktop/src/main/ipc/config-transfer.ipc.ts");
  registerConfigTransferIPC();
  const { coreAIConfigService, coreIllustrationStyleService } = await import("@guizhi/core");
  const { decryptConfigSecrets } = await import("../../apps/desktop/src/main/services/config-transfer/index.ts");
  const { BUILT_IN_ILLUSTRATION_STYLES } = await import("../../packages/core/src/illustration-styles.ts");

  const ctDir = path.join(ctx.out, "ct-files");
  rmrf(ctDir);
  fs.mkdirSync(ctDir, { recursive: true });

  // 种子配置：用真实服务写出，保证文件内容与 TS 版逐字节一致
  const prov = (id: string, provider: string, apiUrl: string, apiKey: string) => ({ id, provider, name: provider, apiProtocol: "openai" as const, apiKey, apiUrl });
  const model = (id: string, providerId: string, provider: string, apiUrl: string, name: string, apiKey: string) => ({ id, providerId, provider, apiProtocol: "openai" as const, apiKey, apiUrl, model: name });
  const seedAi = (withLocal: boolean) => {
    const providers = [prov("p-main", "OpenAI", "https://api.example.com/v1", "sk-local-main")];
    const models = [model("m-main", "p-main", "OpenAI", "https://api.example.com/v1", "gpt-x", "sk-local-main")];
    if (withLocal) {
      providers.push(prov("provider_local_funasr", "FunASR", "http://127.0.0.1:8620/v1", "local"));
      models.push(model("model_local_sensevoice", "provider_local_funasr", "FunASR", "http://127.0.0.1:8620/v1", "sensevoice", "local"));
    }
    coreAIConfigService.replace({ providers, models, modelRouteDefaults: withLocal ? { mainText: "m-main", audioText: "model_local_sensevoice" } : { mainText: "m-main" } });
    return fs.readFileSync(path.join(cfgDir, "ai-models.json"), "utf8");
  };
  const customStyles = [{ id: "mine", name: "我的风格", group: "自定", description: "d", visualDna: "dna", character: "", negative: "n", aspectRatio: "4:3", maxShots: 3, maxLabels: 2 }];
  const seedStyles = () => {
    rmrf(path.join(cfgDir, "illustration-styles.json"));
    coreIllustrationStyleService.write(customStyles);
    return fs.readFileSync(path.join(cfgDir, "illustration-styles.json"), "utf8");
  };
  const mcpSelected = JSON.stringify({ version: 1, mode: "selected", allowedCollectionIds: ["c1", "c2"], allowUncategorized: false }, null, 2) + "\n";

  const seeds: Record<string, Record<string, string>> = {};
  resetEnv();
  const aiPlain = seedAi(false);
  const aiLocal = seedAi(true);
  const stylesCustom = seedStyles();
  resetEnv();
  seeds.empty = {};
  seeds.basic = { "ai-models.json": aiPlain, "illustration-styles.json": stylesCustom, "shortcuts.json": JSON.stringify({ showApp: "Alt+X", newItem: "Alt+Y", search: "Alt+Z", settings: "Alt+W" }, null, 2), "shortcut-mode.json": JSON.stringify({ showApp: "global", newItem: "local", search: "local", settings: "local" }, null, 2), "mcp.json": mcpSelected };
  seeds.local = { ...seeds.basic, "ai-models.json": aiLocal };
  seeds.oldSnapshots = {
    ...seeds.basic,
    "pre-import-20200101-000001/ai-models.json": "{\"s\":1}",
    "pre-import-20200101-000002/ai-models.json": "{\"s\":2}",
    "pre-import-20200101-000003/ai-models.json": "{\"s\":3}",
    "pre-import-20200101-000004/ai-models.json": "{\"s\":4}",
  };
  seeds.brokenAi = { ...seeds.basic, "ai-models.json": "{not json" };
  const mcpFileName = "mcp.json";
  void mcpFileName;

  type CtOp =
    | { op: "export"; request: unknown; save: { canceled: boolean } | { path: string } | null; password?: string }
    | { op: "read"; file?: string; pick?: "cancel" | "missing" }
    | { op: "apply"; password?: string; selection?: unknown; pathMode?: "last" | "other" | "empty" };
  interface CtCase { name: string; seed: string; ops: CtOp[]; files?: Record<string, string> }

  const settingsBase = (extra: Json = {}): Json => ({
    theme: "dark", language: "zh", dataPath: "D:\\data", ytDlpPath: "C:\\yt.exe", ffmpegPath: "C:\\ff.exe", launchAtStartup: true, backgroundImageFileName: "bg.png", settingsUpdatedAt: 5, isDarkMode: true,
    aiApiKey: "sk-legacy", networkProxy: { enabled: true, host: "h", password: "proxy-pw" },
    aiProviders: [prov("p-a", "A", "https://a.example/v1", "sk-a"), prov("p-b", "B", "https://b.example/v1", "")],
    aiModels: [model("m-a", "p-a", "A", "https://a.example/v1", "ma", "sk-ma"), model("m-b", "p-b", "B", "https://b.example/v1", "mb", "")],
    modelRouteDefaults: { mainText: "m-a", fastText: "m-b", audioText: "  " },
    ...extra,
  });
  const exportReq = (extra: Json = {}) => ({ settings: settingsBase(), settingsVersion: 7, uiLayout: { "ui-storage": { a: 1 }, "guizhi-library-table-config": "x", "evil-key": 1, "guizhi-table-config": undefined }, includeSecrets: false, ...extra });

  // 由真实导出产生的「合法文件」，供 read/apply 场景使用（含加密版本）：先跑一遍 TS 导出，把文件文本存下来
  const producedFiles: Record<string, string> = {};
  const produce = async (name: string, seed: string, request: unknown) => {
    resetEnv(seeds[seed]);
    shortcuts.registerShortcuts({ skipGlobal: true });
    const target = path.join(ctDir, `${name}.json`);
    ctx.saveQueue.push({ canceled: false, filePath: target });
    await (ctx.ipcHandlers.get("config:export") as (...a: unknown[]) => unknown)({}, request);
    producedFiles[name] = fs.readFileSync(target, "utf8");
  };
  await produce("plain", "basic", exportReq());
  await produce("enc", "basic", exportReq({ includeSecrets: true, password: "pw-123" }));
  await produce("local-engine", "local", exportReq({ settings: settingsBase({ aiProviders: [...(settingsBase().aiProviders as unknown[]), prov("provider_local_funasr", "FunASR", "http://localhost:8620/v1", "x")], aiModels: [...(settingsBase().aiModels as unknown[]), model("model_local_sensevoice", "provider_local_funasr", "FunASR", "http://127.0.0.1:8620/v1", "sv", "x")], modelRouteDefaults: { chat: "m-a", audioText: "model_local_sensevoice" } }) }));

  const mut = (name: string, fn: (f: Json) => void) => {
    const f = JSON.parse(producedFiles.plain) as Json;
    fn(f);
    return JSON.stringify(f, null, 2) + "\n";
  };
  const files: Record<string, string> = {
    ...producedFiles,
    notJson: "{broken",
    notObject: "[1]",
    wrongKind: JSON.stringify({ kind: "other", version: 1, settings: {} }),
    badVersionType: JSON.stringify({ kind: "guizhi-config", version: "1", settings: {} }),
    version0: JSON.stringify({ kind: "guizhi-config", version: 0, settings: {} }),
    versionFloat: JSON.stringify({ kind: "guizhi-config", version: 1.5, settings: {} }),
    versionNew: JSON.stringify({ kind: "guizhi-config", version: 2, settings: {} }),
    noSettings: JSON.stringify({ kind: "guizhi-config", version: 1 }),
    settingsArray: JSON.stringify({ kind: "guizhi-config", version: 1, settings: [] }),
    encNull: JSON.stringify({ kind: "guizhi-config", version: 1, settings: { a: 1 }, encryption: null }),
    encString: JSON.stringify({ kind: "guizhi-config", version: 1, settings: { a: 1 }, encryption: "x" }),
    encAlgo: mut("encAlgo", (f) => { f.encryption = { algo: "aes-128-gcm", kdf: "scrypt", salt: "AA==", n: 16384, r: 8, p: 1, canary: "x" }; }),
    encIncomplete: mut("encIncomplete", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "", n: 16384, r: 8, p: 1, canary: "x" }; }),
    encN3: mut("encN3", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "AA==", n: 3, r: 8, p: 1, canary: "x" }; }),
    encNsmall: mut("encNsmall", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "AA==", n: 2048, r: 8, p: 1, canary: "x" }; }),
    encNbig: mut("encNbig", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "AA==", n: 131072, r: 8, p: 1, canary: "x" }; }),
    encR9: mut("encR9", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "AA==", n: 16384, r: 9, p: 1, canary: "x" }; }),
    encP5: mut("encP5", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "AA==", n: 16384, r: 8, p: 5, canary: "x" }; }),
    encPfloat: mut("encPfloat", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "AA==", n: 16384, r: 8, p: 1.5, canary: "x" }; }),
    encBadCanary: mut("encBadCanary", (f) => { f.encryption = { algo: "aes-256-gcm", kdf: "scrypt", salt: "AAAAAAAAAAAAAAAAAAAAAA==", n: 16384, r: 8, p: 1, canary: "ENC::AAAA" }; }),
    dirtyEntries: mut("dirtyEntries", (f) => {
      const s = f.settings as Json;
      s.aiProviders = [...(s.aiProviders as unknown[]), { id: "", provider: "x", apiUrl: "u" }, null, 5, { id: "p-ok", provider: "P", apiUrl: "https://ok", apiKey: "k" }, { id: "p-nourl", provider: "P" }];
      s.aiModels = [...(s.aiModels as unknown[]), { id: "m-bad", provider: "P" }, "str", { id: "m-ok", providerId: "p-ok", provider: "P", apiUrl: "https://ok", model: "mm", apiKey: "k" }];
      s.modelRouteDefaults = { mainText: "m-a", fastText: "ghost", audioText: "nope", scene: 5, spaced: " m-ok ", empty: "" };
    }),
    noAiArrays: mut("noAiArrays", (f) => { const s = f.settings as Json; delete s.aiProviders; delete s.aiModels; delete s.modelRouteDefaults; }),
    routesNotObject: mut("routesNotObject", (f) => { (f.settings as Json).modelRouteDefaults = ["m-a"]; }),
    scAndMcpAbsent: mut("scAndMcpAbsent", (f) => { delete f.shortcuts; delete f.mcpScope; delete f.illustrationStyles; }),
    mcpAll: mut("mcpAll", (f) => { f.mcpScope = { version: 1, mode: "all", allowedCollectionIds: [], allowUncategorized: true }; }),
    mcpSelectedEmpty: mut("mcpSelectedEmpty", (f) => { f.mcpScope = { mode: "selected", allowedCollectionIds: [], allowUncategorized: false }; }),
    mcpGarbage: mut("mcpGarbage", (f) => { f.mcpScope = { mode: "weird", allowedCollectionIds: "x" }; }),
    mcpArray: mut("mcpArray", (f) => { f.mcpScope = ["x"]; }),
    shortcutsLegacy: mut("shortcutsLegacy", (f) => { f.shortcuts = { accelerators: { newPrompt: "Alt+9", bogus: "B", search: "" }, modes: { newPrompt: "global", search: "nope" } }; }),
    shortcutsBadTypes: mut("shortcutsBadTypes", (f) => { f.shortcuts = { accelerators: "x", modes: ["a"] }; }),
    stylesInvalid: mut("stylesInvalid", (f) => { f.illustrationStyles = [{ id: "ok", visualDna: "d" }, { id: "", visualDna: "x" }]; }),
    stylesDupe: mut("stylesDupe", (f) => { f.illustrationStyles = [{ id: "a", name: "A", visualDna: "d1" }, { id: "a", name: "A2", visualDna: "d2" }, { id: "a-2", visualDna: "d3", aspectRatio: "bad", maxShots: 99, maxLabels: -1 }]; }),
    stylesEmpty: mut("stylesEmpty", (f) => { f.illustrationStyles = []; }),
    stylesNotArray: mut("stylesNotArray", (f) => { f.illustrationStyles = { a: 1 }; }),
    uiLayoutEvil: mut("uiLayoutEvil", (f) => { f.uiLayout = { "ui-storage": 1, evil: 2, "guizhi-table-config": { x: 1 } }; }),
    uiLayoutArray: mut("uiLayoutArray", (f) => { f.uiLayout = [1, 2]; }),
    settingsStrip: mut("settingsStrip", (f) => { Object.assign(f.settings as Json, { dataPath: "x", ytDlpPath: "y", theme: "light", launchAtStartup: false }); }),
    settingsVersionOnly: mut("settingsVersionOnly", (f) => { f.settingsVersion = 9; f.appVersion = 5; f.exportedAt = 7; }),
  };

  const ctCases: CtCase[] = [
    { name: "apply-without-read", seed: "basic", ops: [{ op: "apply", pathMode: "empty" }] },
    // 导出
    { name: "export-plain", seed: "basic", ops: [{ op: "export", request: exportReq(), save: { path: "export-plain" } }] },
    { name: "export-plain-empty-env", seed: "empty", ops: [{ op: "export", request: exportReq(), save: { path: "export-empty" } }] },
    { name: "export-secrets", seed: "basic", ops: [{ op: "export", request: exportReq({ includeSecrets: true, password: "  pw-ok  " }), save: { path: "export-secrets" }, password: "pw-ok" }] },
    { name: "export-secrets-no-password", seed: "basic", ops: [{ op: "export", request: exportReq({ includeSecrets: true }), save: { path: "x" } }] },
    { name: "export-secrets-blank-password", seed: "basic", ops: [{ op: "export", request: exportReq({ includeSecrets: true, password: "   " }), save: { path: "x" } }] },
    { name: "export-flags-off", seed: "basic", ops: [{ op: "export", request: exportReq({ includeUiLayout: false, includeIllustrationStyles: false, includeShortcuts: false, includeMcpScope: false }), save: { path: "export-off" } }] },
    { name: "export-flags-mixed", seed: "basic", ops: [{ op: "export", request: exportReq({ includeShortcuts: false }), save: { path: "export-mixed" } }] },
    { name: "export-canceled", seed: "basic", ops: [{ op: "export", request: exportReq(), save: { canceled: true } }] },
    { name: "export-no-request", seed: "basic", ops: [{ op: "export", request: undefined, save: { path: "x" } }] },
    { name: "export-no-settings", seed: "basic", ops: [{ op: "export", request: { includeSecrets: false }, save: { path: "export-nosettings" } }] },
    { name: "export-settings-null", seed: "basic", ops: [{ op: "export", request: { settings: null, includeSecrets: false }, save: { path: "export-nullsettings" } }] },
    { name: "export-local-engine-kept", seed: "local", ops: [{ op: "export", request: exportReq(), save: { path: "export-local" } }] },
    { name: "export-broken-ai-config-ok", seed: "brokenAi", ops: [{ op: "export", request: exportReq(), save: { path: "export-brokenai" } }] },
    { name: "export-after-apply", seed: "basic", ops: [{ op: "read", file: "shortcutsLegacy" }, { op: "apply" }, { op: "export", request: exportReq(), save: { path: "export-after-apply" } }] },
    // 读取
    { name: "read-plain", seed: "basic", ops: [{ op: "read", file: "plain" }] },
    { name: "read-enc", seed: "basic", ops: [{ op: "read", file: "enc" }] },
    { name: "read-canceled", seed: "basic", ops: [{ op: "read", pick: "cancel" }] },
    { name: "read-missing-file", seed: "basic", ops: [{ op: "read", pick: "missing" }] },
    ...["notJson", "notObject", "wrongKind", "badVersionType", "version0", "versionFloat", "versionNew", "noSettings", "settingsArray", "encNull", "encString", "encAlgo", "encIncomplete", "encN3", "encNsmall", "encNbig", "encR9", "encP5", "encPfloat", "scAndMcpAbsent", "mcpAll", "mcpSelectedEmpty", "mcpGarbage", "mcpArray", "shortcutsBadTypes", "stylesNotArray", "uiLayoutEvil", "uiLayoutArray", "settingsVersionOnly"].map((file): CtCase => ({ name: `read-${file}`, seed: "basic", ops: [{ op: "read", file }] })),
    // 应用
    { name: "apply-plain-all", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply" }] },
    { name: "apply-plain-twice", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply" }, { op: "apply" }] },
    { name: "apply-other-path", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", pathMode: "other" }] },
    { name: "apply-empty-path", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", pathMode: "empty" }] },
    { name: "apply-enc-no-password", seed: "basic", ops: [{ op: "read", file: "enc" }, { op: "apply" }] },
    { name: "apply-enc-wrong-password", seed: "basic", ops: [{ op: "read", file: "enc" }, { op: "apply", password: "nope" }] },
    { name: "apply-enc-ok", seed: "basic", ops: [{ op: "read", file: "enc" }, { op: "apply", password: " pw-123 " }] },
    { name: "apply-enc-ok-then-plain-pw", seed: "basic", ops: [{ op: "read", file: "enc" }, { op: "apply", password: "pw-123" }, { op: "apply", password: "pw-123" }] },
    { name: "apply-enc-bad-canary", seed: "basic", ops: [{ op: "read", file: "encBadCanary" }, { op: "apply", password: "x" }] },
    { name: "apply-sel-settings-off", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", selection: { settings: false } }] },
    { name: "apply-sel-uilayout-off", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", selection: { uiLayout: false } }] },
    { name: "apply-sel-all-off", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", selection: { settings: false, uiLayout: false, illustrationStyles: false, shortcuts: false, mcpScope: false } }] },
    { name: "apply-sel-styles-sc-mcp-off", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", selection: { illustrationStyles: false, shortcuts: false, mcpScope: false } }] },
    { name: "apply-sel-non-object", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", selection: null }] },
    { name: "apply-sel-truthy-strings", seed: "basic", ops: [{ op: "read", file: "plain" }, { op: "apply", selection: { settings: "false", mcpScope: 0 } }] },
    { name: "apply-local-engine-keeps-local", seed: "local", ops: [{ op: "read", file: "local-engine" }, { op: "apply" }] },
    { name: "apply-plain-onto-local", seed: "local", ops: [{ op: "read", file: "plain" }, { op: "apply" }] },
    { name: "apply-dirty-entries", seed: "basic", ops: [{ op: "read", file: "dirtyEntries" }, { op: "apply" }] },
    { name: "apply-dirty-entries-onto-local", seed: "local", ops: [{ op: "read", file: "dirtyEntries" }, { op: "apply" }] },
    { name: "apply-no-ai-arrays", seed: "local", ops: [{ op: "read", file: "noAiArrays" }, { op: "apply" }] },
    { name: "apply-routes-not-object", seed: "basic", ops: [{ op: "read", file: "routesNotObject" }, { op: "apply" }] },
    { name: "apply-sc-mcp-absent", seed: "basic", ops: [{ op: "read", file: "scAndMcpAbsent" }, { op: "apply" }] },
    { name: "apply-mcp-all", seed: "basic", ops: [{ op: "read", file: "mcpAll" }, { op: "apply" }] },
    { name: "apply-mcp-selected-empty", seed: "basic", ops: [{ op: "read", file: "mcpSelectedEmpty" }, { op: "apply" }] },
    { name: "apply-mcp-garbage", seed: "basic", ops: [{ op: "read", file: "mcpGarbage" }, { op: "apply" }] },
    { name: "apply-shortcuts-legacy-keys", seed: "basic", ops: [{ op: "read", file: "shortcutsLegacy" }, { op: "apply" }] },
    { name: "apply-shortcuts-bad-types", seed: "basic", ops: [{ op: "read", file: "shortcutsBadTypes" }, { op: "apply" }] },
    { name: "apply-styles-invalid", seed: "basic", ops: [{ op: "read", file: "stylesInvalid" }, { op: "apply" }] },
    { name: "apply-styles-dupe-ids", seed: "basic", ops: [{ op: "read", file: "stylesDupe" }, { op: "apply" }] },
    { name: "apply-styles-empty", seed: "basic", ops: [{ op: "read", file: "stylesEmpty" }, { op: "apply" }] },
    { name: "apply-styles-not-array", seed: "basic", ops: [{ op: "read", file: "stylesNotArray" }, { op: "apply" }] },
    { name: "apply-uilayout-evil", seed: "basic", ops: [{ op: "read", file: "uiLayoutEvil" }, { op: "apply" }] },
    { name: "apply-settings-strip", seed: "basic", ops: [{ op: "read", file: "settingsStrip" }, { op: "apply" }] },
    { name: "apply-settings-version-only", seed: "basic", ops: [{ op: "read", file: "settingsVersionOnly" }, { op: "apply" }] },
    { name: "apply-empty-env", seed: "empty", ops: [{ op: "read", file: "plain" }, { op: "apply" }] },
    { name: "apply-old-snapshots-pruned", seed: "oldSnapshots", ops: [{ op: "read", file: "plain" }, { op: "apply" }] },
    { name: "apply-broken-ai-config", seed: "brokenAi", ops: [{ op: "read", file: "plain" }, { op: "apply" }] },
  ];

  const normPath = (value: unknown): unknown => {
    if (typeof value !== "string") return value;
    let s = value.replace(STAMP_RE, (m) => (m >= "20250101-000000" ? "STAMP" : m));
    s = s.split(ctDir).join("<CT>").split(cfgDir).join("<CFG>").split(ctx.userData).join("<UD>");
    return s.replace(/\\/g, "/");
  };
  const normResult = (r: unknown): unknown => JSON.parse(JSON.stringify(r ?? null, (_k, v) => (typeof v === "string" ? normPath(v) : v)));

  const ctOut: unknown[] = [];
  for (const c of ctCases) {
    resetEnv(seeds[c.seed]);
    shortcuts.registerShortcuts({ skipGlobal: true });
    ctx.dialogCalls.length = 0;
    const opsSnapshot = JSON.parse(JSON.stringify(c.ops)); // 处理器会就地改写请求（如加密机密字段），golden 只能记录执行前的样子
    const results: unknown[] = [];
    let lastPicked = "";
    const exportedFiles: Record<string, string> = {};
    for (const op of c.ops) {
      ctx.saveQueue.length = 0; // 处理器若在弹窗前就失败，上一个场景遗留的队列项会错位
      ctx.openQueue.length = 0;
      if (op.op === "export") {
        const target = op.save && "path" in op.save ? path.join(ctDir, `${op.save.path}.json`) : "";
        if (op.save && "path" in op.save) { rmrf(target); ctx.saveQueue.push({ canceled: false, filePath: target }); }
        else if (op.save) ctx.saveQueue.push({ canceled: true });
        const res = await callH("config:export", op.request);
        const rec: Json = { result: normResult(res) };
        if (target && fs.existsSync(target)) {
          const text = fs.readFileSync(target, "utf8");
          exportedFiles[op.save && "path" in op.save ? op.save.path : ""] = text;
          const parsed = JSON.parse(text) as Json;
          const stable = (o: Json) => { if ("exportedAt" in o) o.exportedAt = "NOW"; if ("appVersion" in o) o.appVersion = "VER"; return o; };
          rec.raw = text.replace(/"exportedAt": "[^"]*"/, '"exportedAt": "NOW"').replace(/"appVersion": "[^"]*"/, '"appVersion": "VER"');
          if (parsed.encryption && op.password) {
            const copy = JSON.parse(text);
            const dec = decryptConfigSecrets(copy, op.password);
            rec.decryptOk = dec.ok;
            rec.decryptFailed = dec.failed ?? 0;
            rec.encryption = { ...(parsed.encryption as Json), salt: "SALT", canary: "CANARY" };
            rec.plain = stable(copy);
            delete rec.raw;
          } else {
            rec.plain = stable(parsed);
          }
        }
        const calls = ctx.dialogCalls.filter((d) => d.kind === "save").map((d) => ({ title: d.options.title, filters: d.options.filters, defaultName: String(d.options.defaultPath ?? "").split(/[\\/]/).pop()!.replace(STAMP_RE, "STAMP") }));
        rec.dialog = calls;
        results.push(rec);
      } else if (op.op === "read") {
        if (op.pick === "cancel") ctx.openQueue.push({ canceled: true, filePaths: [] });
        else if (op.pick === "missing") { lastPicked = path.join(ctDir, "does-not-exist.json"); ctx.openQueue.push({ canceled: false, filePaths: [lastPicked] }); }
        else {
          lastPicked = path.join(ctDir, `read-${c.name}.json`);
          fs.writeFileSync(lastPicked, files[op.file!], "utf8");
          ctx.openQueue.push({ canceled: false, filePaths: [lastPicked] });
        }
        results.push({ result: normResult(await callH("config:read")) });
      } else {
        const filePath = op.pathMode === "empty" ? "" : op.pathMode === "other" ? path.join(ctDir, "other.json") : lastPicked;
        const args: unknown[] = [filePath, op.password];
        if ("selection" in op) args.push(op.selection);
        const res = await callH("config:apply", ...args);
        results.push({ result: normResult(res) });
      }
    }
    ctOut.push({ name: c.name, seed: c.seed, ops: opsSnapshot, results, after: dumpEnv() });
  }

  return {
    shortcuts: { scenarios: scenariosOut },
    configTransfer: {
      seeds, files, builtinStyles: BUILT_IN_ILLUSTRATION_STYLES, scenarios: ctOut,
    },
  };
}
