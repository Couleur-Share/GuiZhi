import { afterAll, describe, expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import path from "node:path";
import os from "node:os";
import samples from "../../../../config/crawl4ai/public-samples.json";
import {
  captureWebPage,
  shutdownWebCapture,
} from "../../src/main/services/web-capture/web-capture";
import { configureRuntimePaths } from "../../src/main/runtime-paths";
import { fetchHtml } from "../../src/main/services/import/safe-fetch";
import { Readability } from "@mozilla/readability";
import { parseHTML } from "linkedom";
import { applyNetworkProxySettings } from "../../src/main/services/network-proxy";
vi.mock("electron", () => ({
  app: { isPackaged: false, getAppPath: () => process.cwd() },
  session: {
    defaultSession: {
      resolveProxy: async () => "DIRECT",
      setProxy: async () => undefined,
    },
  },
}));
const enabled = process.env.GUIZHI_TEST_LIVE_CRAWLER === "1";
let directory: string;
afterAll(async () => {
  await shutdownWebCapture();
  if (directory) await fs.rm(directory, { recursive: true, force: true });
});
describe.skipIf(!enabled)("公开网址对照（显式开启，不调用模型）", () => {
  it("记录 40 个固定网址的新旧正文与失败边界", async () => {
    directory = await fs.mkdtemp(path.join(os.tmpdir(), "guizhi-web-live-"));
    configureRuntimePaths({ userDataPath: directory });
    if (process.env.GUIZHI_TEST_HTTP_PROXY) {
      const proxy = new URL(process.env.GUIZHI_TEST_HTTP_PROXY);
      await applyNetworkProxySettings({
        mode: "manual",
        protocol: "http",
        host: proxy.hostname,
        port: Number(proxy.port),
        bypass: "",
      });
    }
    const output = path.resolve("../../artifacts/crawl4ai/public-results.json");
    const rows: any[] =
      process.env.GUIZHI_TEST_RESUME === "1"
        ? JSON.parse(await fs.readFile(output, "utf8"))
        : [];
    if (process.env.GUIZHI_TEST_RETRY_FAILURES === "1")
      for (let i = rows.length - 1; i >= 0; i--)
        if (rows[i].result?.error || !rows[i].expectedMatched)
          rows.splice(i, 1);
    const remaining = samples.filter(
      (s) => !rows.some((r) => r.sample.url === s.url),
    );
    let index = 0;
    const consume = async () => {
      while (index < remaining.length) {
        const current = index++,
          sample = remaining[current];
        const started = Date.now();
        let baseline = "",
          baselineError = "";
        try {
          const html = await fetchHtml(sample.url, AbortSignal.timeout(20000));
          const { document } = parseHTML(html.html);
          baseline =
            new Readability(document as unknown as Document, {
              charThreshold: 100,
            }).parse()?.textContent ?? "";
        } catch (e) {
          baselineError = e instanceof Error ? e.message : String(e);
        }
        try {
          const result = await captureWebPage(
            { taskId: `sample-${current}`, purpose: "import", url: sample.url },
            AbortSignal.timeout(60000),
          );
          rows.push({
            sample,
            ms: Date.now() - started,
            baselineChars: baseline.length,
            baselineContainsExpected:
              !!sample.expect && baseline.includes(sample.expect),
            baselineError,
            result,
            expectedMatched: sample.expectError
              ? !!result.error &&
                result.error.code === sample.expectedCode &&
                (!sample.expectedMessage ||
                  result.error.message.includes(sample.expectedMessage))
              : result.complete &&
                result.markdown.includes(sample.expect ?? ""),
          });
        } catch (e) {
          rows.push({
            sample,
            ms: Date.now() - started,
            baselineChars: baseline.length,
            baselineError,
            error: e instanceof Error ? e.message : String(e),
            expectedMatched: false,
          });
        }
        await fs.mkdir(path.resolve("../../artifacts/crawl4ai"), {
          recursive: true,
        });
        await fs.writeFile(
          path.resolve("../../artifacts/crawl4ai/public-results.json"),
          JSON.stringify(rows, null, 2),
        );
      }
    };
    await Promise.all([consume(), consume()]);
    expect(rows).toHaveLength(40);
  }, 1800000);
});
