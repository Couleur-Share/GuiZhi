import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import benchmark from "../../apps/desktop/tests/e2e/reading-v3-benchmark.mjs";

test("联网验收必须同时完成正文、摘要和真实来源；失败记录仍保存", async () => {
  const keys = ["GUIZHI_READING_BENCH_CONFIG", "GUIZHI_READING_BENCH_SAMPLES", "GUIZHI_READING_BENCH_PROFILE"];
  const previous = keys.map(key => process.env[key]);
  const outDir = await fs.mkdtemp(path.join(os.tmpdir(), "guizhi-benchmark-gate-"));
  try {
    keys.forEach(key => process.env[key] = "fixture");
    process.env.GUIZHI_READING_BENCH_PROFILE = "release-online";
    const completed = ["body", "summary"].map(sourceKind => ({status:"completed", readyReferences:1, sourceKind}));
    const run = results => benchmark({ app: {evaluate:async () => results}, shot:async () => {}, outDir });
    await run(completed);
    for (const results of [[], completed.slice(0,1),
      [completed[0], {...completed[1], status:"failed"}],
      [completed[0], {...completed[1], readyReferences:0}],
      [completed[0], completed[0]]]) {
      await assert.rejects(run(results), /发版联网验收未通过/);
      assert.deepEqual(JSON.parse(await fs.readFile(path.join(outDir,"results.json"),"utf8")), results);
    }
  } finally {
    keys.forEach((key,index) => { if(previous[index] === undefined) delete process.env[key]; else process.env[key] = previous[index]; });
    await fs.rm(outDir,{recursive:true,force:true});
  }
});
