import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

export default async function ({ win, app, shot, outDir }) {
  const runtime = await app.evaluate(({ app }) => ({
    packaged: app.isPackaged,
    version: app.getVersion(),
    userData: app.getPath("userData"),
  }));
  assert.equal(runtime.packaged, true);
  assert.equal(runtime.version, "0.22.0");
  const fixture = await win.evaluate(async () => {
    const collection = await window.api.collection.create({
      name: "升级验收集合",
    });
    const item = await window.api.knowledge.create({
      title: "升级前的手动标题",
      content: "# 旧版正文\n\n人工编辑必须保留。",
      itemType: "webpage",
      collectionId: collection.id,
      tagNames: ["升级验收", "人工编辑"],
    });
    await window.api.knowledge.update(item.id, {
      isFavorite: true,
      summary: "旧版摘要，应当保留。",
    });
    const backup = await window.api.backup.create();
    if (!backup.success) throw new Error(backup.error);
    return {
      item: await window.api.knowledge.get(item.id),
      collection,
      backup,
    };
  });
  assert.equal(fixture.item.content, "# 旧版正文\n\n人工编辑必须保留。");
  fs.writeFileSync(
    path.join(outDir, "previous.json"),
    JSON.stringify({ runtime, ...fixture }, null, 2),
  );
  await shot("released-app");
}
