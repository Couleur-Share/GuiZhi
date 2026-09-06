import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

export default async function ({ win, app, shot, outDir }) {
  const previous = JSON.parse(
    fs.readFileSync(process.env.GUIZHI_ACCEPTANCE_PREVIOUS, "utf8"),
  );
  const runtime = await app.evaluate(({ app }) => ({
    packaged: app.isPackaged,
    userData: app.getPath("userData"),
  }));
  assert.equal(runtime.packaged, true);
  const result = await win.evaluate(
    async (id) => ({
      item: await window.api.knowledge.get(id),
      backups: await window.api.backup.list(),
      versions: await window.api.webCapture.versions(id),
      status: await window.api.webCapture.status(),
    }),
    previous.item.id,
  );
  assert.deepEqual(result.item, previous.item);
  assert.equal(result.versions.ok, true);
  assert.equal(result.versions.data.versions.length, 0);
  fs.writeFileSync(
    path.join(outDir, "upgrade.json"),
    JSON.stringify({ runtime, ...result }, null, 2),
  );
  await shot("upgraded-app");
  assert.equal(
    result.backups.filter((b) => b.kind === "pre-update").length,
    1,
    "升级前缺少完整数据库快照",
  );
}
