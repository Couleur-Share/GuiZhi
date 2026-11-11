// 本地转写引擎（FunASR）差分基线：纯函数与文件系统约定逐项由真实 TS 产出，Go 侧逐项比对。
// 涵盖：路径布局、版本比较、日志摘要、代理错误改写、目录身份（libuv 口径）、环境体积估算、
// 备份清单格式与备份清理的判定，以及「内置转写模型」写入/移除 ai-models.json 的结果。
import fs from "node:fs";
import path from "node:path";

const UPDATED_AT = /"updatedAt": "[^"]*"/g;
const VERIFIED_AT = /"lastVerifiedAt": "[^"]*"/g;

export async function buildFunasrGolden(out: string): Promise<unknown> {
  const { getFunasrPaths } = await import("../../apps/desktop/src/main/services/media/funasr-paths.ts");
  const { isNewerFunasrVersion } = await import("../../apps/desktop/src/main/services/media/funasr-update.ts");
  const { summarizeProcessLog } = await import("../../apps/desktop/src/main/services/media/funasr-service.ts");
  const { explainFunasrProxyFailure } = await import("../../apps/desktop/src/main/services/media/funasr-proxy.ts");
  const { readDirectoryIdentity } = await import("../../apps/desktop/src/main/services/media/funasr-update-paths.ts");
  const { measureFunasrEnvironment, formatFunasrBytes } = await import("../../apps/desktop/src/main/services/media/funasr-update-storage.ts");
  const backupMod = await import("../../apps/desktop/src/main/services/media/funasr-update-backup.ts");
  const { upsertBuiltinTranscription, removeBuiltinTranscription } = await import("../../apps/desktop/src/main/services/media/funasr-manager.ts");
  const { coreAIConfigService, getAIConfigFilePath } = await import("@guizhi/core");

  // ── 路径布局 ──
  const rootForPaths = "D:\\fake\\tools\\funasr";
  const paths = getFunasrPaths(rootForPaths);

  // ── 版本比较 ──
  const versionPairs: [string, string][] = [
    ["1.3.30", "1.3.29"], ["1.3.29", "1.3.29"], ["1.3.28", "1.3.29"], ["1.4", "1.3.99"], ["2.0", "1.99.99"],
    ["1.3.29.1", "1.3.29"], ["1.3", "1.3.0"], ["1.10.0", "1.9.9"], ["1.0.0", "1.0"],
    ["1.3.29rc1", "1.3.29"], ["1.3.29", ""], ["latest", "1.0"], ["1..3", "1.3"], ["01.2", "1.2"],
  ];
  const versions = versionPairs.map(([latest, current]) => {
    try {
      return { latest, current, newer: isNewerFunasrVersion(latest, current) };
    } catch (e) {
      return { latest, current, error: (e as Error).message };
    }
  });

  // ── 日志摘要 ──
  const bar = "rtf_avg: 0.043: 100%|██████████| 1/1 [00:01<00:00,  1.28s/it]";
  const logs = [
    "",
    "plain line\nsecond line",
    `${bar}\nTraceback (most recent call last):\n  File "x.py", line 1\nRuntimeError: 模型加载失败\n${bar}`,
    `\u001b[31mred error\u001b[0m\r\nok line\r\n`,
    "a\nb\nc\nd\ne\nf",
    "spin 10%|#\rspin 50%|#####\rfinal text visible\n",
    "x".repeat(500),
    "中文".repeat(200) + "\nend",
    "Missing dependencies for SOCKS support.",
    "  \n\t\n",
    "download 3.2it/s]\nreal: 1.28s/it]\nnormal",
  ].map((raw) => ({ raw, summary: summarizeProcessLog(raw), short: summarizeProcessLog(raw, 20) }));
  const proxyMsgs = ["Missing dependencies for SOCKS support", "missing DEPENDENCIES for socks support!", "other failure", ""];
  const proxy = proxyMsgs.map((m) => ({ m, out: explainFunasrProxyFailure(m) }));

  // ── 目录身份 ──
  const idDir = path.join(out, "funasr-idprobe");
  fs.mkdirSync(idDir, { recursive: true });
  const identity = await readDirectoryIdentity(idDir);

  // ── 环境体积 ──
  const treeDir = path.join(out, "funasr-measure");
  fs.mkdirSync(path.join(treeDir, "a", "b"), { recursive: true });
  fs.mkdirSync(path.join(treeDir, "empty"), { recursive: true });
  fs.writeFileSync(path.join(treeDir, "zero.bin"), "");
  fs.writeFileSync(path.join(treeDir, "one.bin"), "x");
  fs.writeFileSync(path.join(treeDir, "a", "exact.bin"), Buffer.alloc(4096));
  fs.writeFileSync(path.join(treeDir, "a", "b", "over.bin"), Buffer.alloc(4097));
  fs.writeFileSync(path.join(treeDir, "a", "b", "big.bin"), Buffer.alloc(100_000));
  const measure = {
    b512: await measureFunasrEnvironment(treeDir, 512),
    b4096: await measureFunasrEnvironment(treeDir, 4096),
    b65536: await measureFunasrEnvironment(treeDir, 65536),
  };
  const formats = [0, 1, 1023, 1024 ** 3, 1.5 * 1024 ** 3, 3_221_225_472, 10 * 1024 ** 3 + 123456789].map((n) => ({ n, s: formatFunasrBytes(n) }));

  // ── 备份清单 ──
  const root = path.join(out, "funasr-backup-root");
  fs.mkdirSync(root, { recursive: true });
  const rootIdentity = await readDirectoryIdentity(root);
  const state = { funasrVersion: "1.3.29", pythonVersion: "Python 3.12.13", flavor: "python" as const, installedAt: "2026-01-02T03:04:05.000Z" };
  const phases = ["copying", "ready", "updating", "recovery-required", "disposable"] as const;
  const manifests: Record<string, string> = {};
  const labelOf = new Map<string, string>();
  const dirs: Record<string, string> = {};
  const backupList: Array<{ label: string; backup: Awaited<ReturnType<typeof backupMod.createFunasrBackup>> }> = [];
  for (const phase of phases) {
    const b = await backupMod.createFunasrBackup(root, state, "1.3.30");
    await backupMod.markFunasrBackup(b, "ready");
    if (phase !== "ready") await backupMod.markFunasrBackup(b, phase);
    // 与 cleanup 判定一致：disposable 的备份只能含 env / tmp / failed-env / update.json
    fs.mkdirSync(path.join(b.directory, "env"));
    fs.writeFileSync(path.join(b.directory, "env", "f.txt"), "x");
    labelOf.set(path.basename(b.directory), phase);
    dirs[phase] = path.basename(b.directory);
    manifests[phase] = fs.readFileSync(path.join(b.directory, "update.json"), "utf8");
    backupList.push({ label: phase, backup: b });
  }
  // 含非预期内容的 disposable：必须保留
  const dirty = await backupMod.createFunasrBackup(root, state, "1.3.30");
  await backupMod.markFunasrBackup(dirty, "disposable");
  fs.writeFileSync(path.join(dirty.directory, "note.txt"), "keep me");
  labelOf.set(path.basename(dirty.directory), "dirty-disposable");
  // 没有清单的 update-backup-* 与 pip-repair-backup-*
  fs.mkdirSync(path.join(root, "update-backup-zzzzzz"));
  labelOf.set("update-backup-zzzzzz", "no-manifest");
  fs.mkdirSync(path.join(root, "pip-repair-backup-old"));
  labelOf.set("pip-repair-backup-old", "pip-repair");
  // 清单字段与目录名不符
  const forged = await backupMod.createFunasrBackup(root, state, "1.3.30");
  fs.renameSync(forged.directory, path.join(root, "update-backup-forged"));
  labelOf.set("update-backup-forged", "forged-name");

  const mask = (text: string) => text.replace(/\\/g, "/");
  const warnings = (await backupMod.cleanupFunasrBackups(root)).map((w) => {
    let text = mask(w).split(mask(root)).join("<ROOT>");
    for (const [name, label] of labelOf) text = text.split(name).join(`<${label}>`);
    return text;
  });
  const remaining = fs.readdirSync(root).map((n) => labelOf.get(n) ?? n).sort();
  const labels = Object.fromEntries(labelOf);

  // 另起一个只含 TS 写出的 disposable 备份的根目录，不由 TS 清理：留给 Go 验证「能认出并清掉旧版留下的备份」。
  // 备份目录的身份绑定创建它的文件系统节点，所以必须原地清理，不能复制后再测。
  const disposableRoot = path.join(out, "funasr-disposable-root");
  fs.mkdirSync(disposableRoot, { recursive: true });
  const disposable = await backupMod.createFunasrBackup(disposableRoot, state, "1.3.30");
  await backupMod.markFunasrBackup(disposable, "disposable");
  fs.mkdirSync(path.join(disposable.directory, "env"));
  fs.writeFileSync(path.join(disposable.directory, "env", "f.txt"), "x");
  fs.writeFileSync(path.join(disposable.directory, "env", "g.txt"), "y");
  const disposableDir = path.basename(disposable.directory);

  // ── 内置转写模型写入 / 移除 ──
  const vAt = "2026-01-01T00:00:00.000Z";
  const prov = (id: string, provider: string, apiUrl: string, extra: Record<string, unknown> = {}) => ({ id, provider, name: provider, apiProtocol: "openai" as const, apiKey: "k", apiUrl, ...extra });
  const mod = (id: string, providerId: string, provider: string, apiUrl: string, name: string, extra: Record<string, unknown> = {}) => ({ id, providerId, provider, apiProtocol: "openai" as const, apiKey: "k", apiUrl, model: name, ...extra });
  const L = "http://127.0.0.1:8620/v1";
  const seeds: Array<{ name: string; providers: unknown[]; models: unknown[]; routes: Record<string, string>; op: "upsert" | "remove"; empty?: boolean }> = [
    { name: "emptyUpsert", providers: [], models: [], routes: {}, op: "upsert", empty: true },
    { name: "plainUpsert", providers: [prov("p1", "OpenAI", "https://a.test/v1")], models: [mod("m1", "p1", "OpenAI", "https://a.test/v1", "gpt")], routes: { mainText: "m1", audioText: "m1" }, op: "upsert" },
    {
      name: "reUpsert",
      providers: [prov("provider_local_funasr", "FunASR", L, { enabled: false }), prov("p1", "OpenAI", "https://a.test/v1")],
      models: [mod("model_local_sensevoice", "provider_local_funasr", "FunASR", L, "old", { isDefault: true, enabled: false }), mod("m1", "p1", "OpenAI", "https://a.test/v1", "gpt")],
      routes: { mainText: "m1" },
      op: "upsert",
    },
    {
      name: "removeRouted",
      providers: [prov("p1", "OpenAI", "https://a.test/v1"), prov("provider_local_funasr", "FunASR", L)],
      models: [mod("m1", "p1", "OpenAI", "https://a.test/v1", "gpt"), mod("model_local_sensevoice", "provider_local_funasr", "FunASR", L, "sensevoice")],
      routes: { mainText: "m1", audioText: "model_local_sensevoice" },
      op: "remove",
    },
    {
      name: "removeOtherRoute",
      providers: [prov("provider_local_funasr", "FunASR", L)],
      models: [mod("model_local_sensevoice", "provider_local_funasr", "FunASR", L, "sensevoice"), mod("m1", "x", "OpenAI", "https://a.test/v1", "gpt")],
      routes: { audioText: "m1" },
      op: "remove",
    },
    { name: "removeAbsent", providers: [prov("p1", "OpenAI", "https://a.test/v1")], models: [], routes: {}, op: "remove" },
  ];
  const cfgFile = getAIConfigFilePath();
  const aiCases = seeds.map((s) => {
    fs.rmSync(cfgFile, { force: true });
    let seedText = "";
    if (!s.empty) {
      coreAIConfigService.replace({ providers: s.providers as never, models: s.models as never, modelRouteDefaults: s.routes as never });
      seedText = fs.readFileSync(cfgFile, "utf8");
    }
    const current = coreAIConfigService.read();
    const next = s.op === "upsert" ? upsertBuiltinTranscription(current, vAt) : removeBuiltinTranscription(current);
    coreAIConfigService.write(next);
    const after = fs.readFileSync(cfgFile, "utf8").replace(UPDATED_AT, '"updatedAt": "NOW"').replace(VERIFIED_AT, '"lastVerifiedAt": "NOW"');
    return { name: s.name, op: s.op, seedText, after };
  });
  fs.rmSync(cfgFile, { force: true });

  return {
    rootForPaths,
    paths,
    versions,
    logs,
    proxy,
    idDir,
    identity,
    treeDir,
    measure,
    formats,
    backup: { root, rootIdentity, state, manifests, dirs, labels, warnings, remaining, disposableRoot, disposableDir },
    aiCases,
  };
}
