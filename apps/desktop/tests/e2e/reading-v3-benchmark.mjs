import fs from "node:fs/promises";
import path from "node:path";
export default async ({ app, shot, outDir }) => {
  const config = process.env.GUIZHI_READING_BENCH_CONFIG,
    samples = process.env.GUIZHI_READING_BENCH_SAMPLES;
  if (!config || !samples) throw new Error("未明确指定只读配置及公开样例");
  const results = await app.evaluate(
    async (_e, input) => globalThis.readingV3Benchmark(input),
    {
      directory: outDir,
      config,
      samples,
      searchConfig: process.env.GUIZHI_READING_BENCH_SEARCH,
      anomalyLedger: process.env.GUIZHI_READING_BENCH_ANOMALY_LEDGER,
      profile: process.env.GUIZHI_READING_BENCH_PROFILE,
      modelId: process.env.GUIZHI_READING_BENCH_MODEL,
      resumeDirectory: process.env.GUIZHI_READING_BENCH_RESUME,
    },
  );
  await fs.writeFile(
    path.join(outDir, "results.json"),
    JSON.stringify(results, null, 2),
  );
  console.log(JSON.stringify(results));
  await shot("benchmark-finished");
  if (process.env.GUIZHI_READING_BENCH_PROFILE === "release-online") {
    const complete = results.length === 2 &&
      results.every(result => result.status === "completed" && result.readyReferences > 0) &&
      new Set(results.map(result => result.sourceKind)).size === 2;
    if (!complete) throw new Error("发版联网验收未通过；失败与用量记录已保存，禁止自动重复生成");
  }
};
