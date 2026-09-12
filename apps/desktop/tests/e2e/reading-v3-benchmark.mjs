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
    },
  );
  await fs.writeFile(
    path.join(outDir, "results.json"),
    JSON.stringify(results, null, 2),
  );
  console.log(JSON.stringify(results));
  await shot("benchmark-finished");
};
