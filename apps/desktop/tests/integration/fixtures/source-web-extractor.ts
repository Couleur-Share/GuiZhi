import path from "node:path";
import { Worker } from "node:worker_threads";
import type * as Extractor from "../../../src/main/services/web-capture/web-node-extractor";

/** Vitest 里没有打包后的提取线程，改用 tsx 从源码启动；其余行为与正式模块一致。 */
export function sourceExtractorModule(
  actual: typeof Extractor,
): typeof Extractor {
  const bootstrap = path.resolve(
    __dirname,
    "../../unit/web-capture/fixtures/web-extract-worker-bootstrap.cjs",
  );
  const entry = path.resolve(
    __dirname,
    "../../../src/main/services/web-capture/web-extract-worker.ts",
  );
  const shared = new actual.WebNodeExtractor(
    async () => new Worker(bootstrap, { workerData: { entry } }),
  );
  return { ...actual, webNodeExtractor: () => shared };
}
