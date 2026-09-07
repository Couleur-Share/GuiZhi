import { afterAll, expect, it, vi } from "vitest";
import fs from "node:fs/promises";
import path from "node:path";
import os from "node:os";
import { extractContent } from "../../src/main/services/import/connectors";
import { configureRuntimePaths } from "../../src/main/runtime-paths";
vi.mock("electron", () => ({ app: { isPackaged: false, getAppPath: () => process.cwd() }, session: { defaultSession: { resolveProxy: async () => "DIRECT", setProxy: async () => undefined } } }));
import { applyNetworkProxySettings } from "../../src/main/services/network-proxy";
let directory: string;
afterAll(async () => { if (directory) await fs.rm(directory, { recursive: true, force: true }); });
it.skipIf(process.env.GUIZHI_TEST_WECHAT_LIVE !== "1")("真实公众号正文与图片采集", async () => {
  directory = await fs.mkdtemp(path.join(os.tmpdir(), "guizhi-wechat-"));
  configureRuntimePaths({ userDataPath: directory });
  if (process.env.GUIZHI_TEST_HTTP_PROXY) {
    const proxy = new URL(process.env.GUIZHI_TEST_HTTP_PROXY);
    await applyNetworkProxySettings({ mode: "manual", protocol: "http", host: proxy.hostname, port: Number(proxy.port), bypass: "" });
  }
  const samples=[];
  for (const sampleUrl of ["https://mp.weixin.qq.com/s/_2kC-fXw7UjneZSrsC9CVQ","https://mp.weixin.qq.com/s/DYbIqFsoZLHU5u9GiUmlZw"]) {
    try {
      const sample=await extractContent("url",sampleUrl);
      samples.push({url:sampleUrl,title:sample.title,characters:sample.content.length,assets:sample.webCapture?.snapshot?.assets.length,failures:sample.webCapture?.snapshot?.failures,warnings:sample.webCapture?.warnings});
    } catch(error) {samples.push({url:sampleUrl,error:String(error)});}
  }
  await fs.mkdir("../../artifacts/wechat-repair", { recursive: true });
  await fs.writeFile("../../artifacts/wechat-repair/additional-samples.json",JSON.stringify(samples,null,2));
  expect(samples.every(sample=>!("error" in sample))).toBe(true);
  const result = await extractContent("url", "https://mp.weixin.qq.com/s/wFgc0MsEKjAuJcPgpf93Pw");
  await fs.mkdir("../../artifacts/wechat-repair", { recursive: true });
  await fs.writeFile("../../artifacts/wechat-repair/live.json", JSON.stringify(result, null, 2));
  expect(result.degradedReason).toBeUndefined();
  expect(result.content).toContain("黄山是我去了两次");
  expect(result.content).toContain("local-image://wechat-");
  expect(result.webCapture.snapshot.assets).toHaveLength(20);
  expect(result.webCapture.snapshot.failures).toEqual([]);
  expect(result.webCapture.snapshot.html).toContain("text-align:center");
  expect(result.content.length).toBeGreaterThan(2000);
}, 600000);
