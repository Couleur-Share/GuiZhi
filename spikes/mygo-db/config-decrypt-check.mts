// 校验 Go 壳加密的配置文件能被 TS 原实现解密：
//   GUIZHI_DUMP_DIR=<dir> go test -run TestConfigExportEncryptedDump .   （在 mygo-shell 下）
//   pnpm dlx tsx spikes/mygo-db/config-decrypt-check.mts <dir>/go-encrypted-config.json
import fs from "node:fs";

const { decryptConfigSecrets } = await import("../../apps/desktop/src/main/services/config-transfer/index.ts");
const file = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const res = decryptConfigSecrets(file, "口令-pw");
const s = file.settings;
const checks: Array<[string, unknown, unknown]> = [
  ["aiApiKey", s.aiApiKey, "sk-秘密"],
  ["networkProxy.password", s.networkProxy?.password, "代理口令"],
  ["aiProviders[0].apiKey", s.aiProviders?.[0]?.apiKey, "k1"],
  ["aiModels[0].apiKey", s.aiModels?.[0]?.apiKey, "k2"],
];
let bad = 0;
console.log("decrypt result:", JSON.stringify(res));
for (const [name, got, want] of checks) {
  const ok = got === want;
  if (!ok) bad++;
  console.log(ok ? "OK  " : "FAIL", name, ok ? "" : `got=${JSON.stringify(got)} want=${JSON.stringify(want)}`);
}
process.exit(res.ok && bad === 0 ? 0 : 1);