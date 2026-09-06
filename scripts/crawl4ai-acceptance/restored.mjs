import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

export default async function ({ win, shot, outDir }) {
  const before = JSON.parse(
    fs.readFileSync(process.env.GUIZHI_ACCEPTANCE_CAPTURE, "utf8"),
  );
  const result = await win.evaluate(
    async ({ itemId, jobId }) => ({
      item: await window.api.knowledge.get(itemId),
      versions: await window.api.webCapture.versions(itemId),
      job: await window.api.webCapture.get(jobId),
    }),
    { itemId: before.adopted.id, jobId: before.interruptedJobId },
  );
  assert.deepEqual(result.item, before.adopted);
  assert.deepEqual(result.versions.data, before.history);
  assert.equal(result.job.data.job.status, "interrupted");
  assert.ok(result.job.data.pages.every((p) => p.status === "pending"));
  await new Promise((resolve) => setTimeout(resolve, 1500));
  assert.equal(
    (
      await win.evaluate(
        (id) => window.api.webCapture.get(id),
        before.interruptedJobId,
      )
    ).data.job.status,
    "interrupted",
  );
  fs.writeFileSync(
    path.join(outDir, "restored.json"),
    JSON.stringify(result, null, 2),
  );
  await win.evaluate(() => {
    const settings = JSON.parse(
      localStorage.getItem("guizhi-settings") || '{"state":{}}',
    );
    settings.state.language = "zh";
    localStorage.setItem("guizhi-settings", JSON.stringify(settings));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  await win.reload();
  await win.getByTestId("topbar-search").waitFor();
  await win.getByText("人工标题", { exact: true }).first().click();
  await win
    .getByRole("button", { name: /原文版本/ })
    .first()
    .click();
  await win.getByRole("button", { name: "原文版本", exact: true }).click();
  await win
    .getByRole("option", { name: /本地快照/ })
    .first()
    .click();
  await win.getByText("所选版本 ·", { exact: false }).waitFor();
  await shot("restored-source-history");
}
