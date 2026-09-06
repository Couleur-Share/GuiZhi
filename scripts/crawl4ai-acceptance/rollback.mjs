import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

export default async function ({ win, app, shot, outDir }) {
  const previous = JSON.parse(
    fs.readFileSync(process.env.GUIZHI_ACCEPTANCE_PREVIOUS, "utf8"),
  );
  assert.equal(await app.evaluate(({ app }) => app.getVersion()), "0.22.0");
  const item = await win.evaluate(
    (id) => window.api.knowledge.get(id),
    previous.item.id,
  );
  assert.deepEqual(item, previous.item);
  fs.writeFileSync(
    path.join(outDir, "rollback.json"),
    JSON.stringify({ passed: true, item }, null, 2),
  );
  await shot("previous-version-with-pre-upgrade-backup");
}
