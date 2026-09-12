/** 仅隔离 Electron 验收可调用。按页面计费尝试先落盘，失败也占一次预算。 */
import fs from "node:fs/promises";
import { writeFileSync, existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { coreAIConfigService } from "@guizhi/core";
import { getUserDataPath } from "@guizhi/core/runtime-paths";
import { resolveMediaSummaryConfig } from "../src/main/services/media/media-summary";
import { resolveImageGenConfig } from "../src/main/services/illustration/image-gen";
import { runWithAiCallSink } from "../src/main/services/ai-call-context";
import { runReadingV3 } from "../src/main/services/themed-reading/v3-pipeline";
import { runReconstruction } from "../src/main/services/themed-reading/reconstruction-pipeline";
import { compileReadingVisuals } from "../src/main/services/themed-reading/visual-compiler";
import { probeReadingPage } from "../src/main/services/themed-reading/v3-views";
import { exportThemedReadingHtml } from "../src/main/services/themed-reading/export";
import { generateThemeAsset } from "../src/main/services/themed-reading/assets";
import { planTheme } from "../src/main/services/themed-reading/design";
import { ensureRequestedReadingImage } from "../src/main/services/themed-reading/required-image";
import { readingV3FixturePage } from "./reading-v3-fixture";
import { readingSearchStatus } from "../src/main/services/themed-reading/search-service";

export async function readingV3Benchmark(input: {
  directory: string;
  config: string;
  samples: string;
  searchConfig?: string;
  anomalyLedger?: string;
}) {
  if (
    process.env.GUIZHI_E2E !== "1" ||
    !getUserDataPath().includes("guizhi-shot-")
  )
    throw new Error("真实验收必须使用截图工具的临时数据目录");
  const directory = path.resolve(input.directory);
  await fs.mkdir(directory, { recursive: true });
  const ledger = path.join(directory, "attempts.json"),
    attempts: any[] = existsSync(ledger)
      ? JSON.parse(readFileSync(ledger, "utf8"))
      : [];
  if (attempts.length)
    throw new Error("真实验收已有记录，禁止自动重复付费生成");
  const saved = JSON.parse(await fs.readFile(input.config, "utf8")),
    originalRead = coreAIConfigService.read;
  coreAIConfigService.read = () => saved;
  try {
    const config = resolveMediaSummaryConfig();
    if (!config) throw new Error("文本模型不可用");
    if (input.searchConfig) {
      await fs.mkdir(path.join(getUserDataPath(), ".machine"), {
        recursive: true,
      });
      await fs.copyFile(
        input.searchConfig,
        path.join(getUserDataPath(), ".machine/reading-search.json"),
      );
      // 在任何付费请求前验证临时 profile 能读取密文，避免把测试配置错误算成模型失败。
      if (!(await readingSearchStatus()).configured)
        throw new Error("隔离验收的搜索配置不可用，尚未发起模型请求");
    }
    const samples = JSON.parse(
      (await fs.readFile(input.samples, "utf8")).replace(/^\uFEFF/, ""),
    );
    let jobs = samples.flatMap((sample) =>
      [2, 3].map((version) => ({
        sample,
        version,
        research: false,
        image: false,
      })),
    );
    if (input.searchConfig)
      jobs.push(
        ...[2, 3].map((version) => ({
          sample: samples[0],
          version,
          research: true,
          image: false,
        })),
      );
    jobs.push({ sample: samples[0], version: 3, research: false, image: true });
    if (input.anomalyLedger) {
      const original = JSON.parse(
        await fs.readFile(input.anomalyLedger, "utf8"),
      );
      if (
        original.length !== 9 ||
        original.some((r) => r.status === "running") ||
        !original.some((r) => r.id === "short-v3" && r.status === "failed") ||
        original.some((r) => /429|限流|频繁/.test(r.error ?? ""))
      )
        throw new Error("异常复测须在九次计划验收结束且未限流后执行");
      jobs = [
        { sample: samples[0], version: 3, research: false, image: false },
      ];
    }
    for (const job of jobs) {
      if (input.anomalyLedger && attempts.length)
        throw new Error("仅剩一次异常复测预算");
      if (attempts.length >= 9)
        throw new Error("九次计划验收预算已用完，保留一次异常复测名额");
      const id = `${job.sample.id}-v${job.version}${job.research ? "-web" : ""}${job.image ? "-image" : ""}${input.anomalyLedger ? "-repeat" : ""}`;
      const page = readingV3FixturePage("");
      Object.assign(page, {
        id,
        formatVersion: job.version,
        design: null,
        reconstruction: {
          notes: [],
          queries: [],
          references: [],
          draft: [],
          interactions: [],
        },
      });
      page.source = {
        title: job.sample.title,
        content: job.sample.content,
        sourceUri: job.sample.url,
        fingerprint: createHash("sha256")
          .update(job.sample.content)
          .digest("hex"),
        blocks: [
          {
            id: "b0",
            markdown: job.sample.content,
            html: "",
            text: job.sample.content,
          },
        ],
      };
      page.options = {
        style:
          "用中文整理技术知识，重点清晰、原文条件完整、适合360px阅读区。用有意义的对比或交互帮助理解，整体克制精致。",
        action: "create",
        research: job.research,
        generateImages: job.image,
        maxImages: job.image ? 1 : 0,
        ...(job.version === 3
          ? { enhancedInteraction: true, researchDepth: "standard" as const }
          : {}),
      };
      const result: any = {
        id,
        model: config.model,
        source: job.sample.url,
        sourceChars: job.sample.content.length,
        startedAt: Date.now(),
        calls: { textCalls: 0, searchCalls: 0, pagesRead: 0, imageCalls: 0 },
        tokens: [],
        status: "running",
      };
      attempts.push(result);
      const persist = () =>
        writeFileSync(ledger, JSON.stringify(attempts, null, 2));
      persist();
      const checkpoint = () => {
        if (page.generation?.sections.length)
          result.firstContentMs ??= Date.now() - result.startedAt;
        writeFileSync(
          path.join(directory, `${id}-checkpoint.json`),
          JSON.stringify(page),
        );
        persist();
      };
      const hooks = {
        checkpoint,
        stage: (stage: string) => {
          result.stage = stage;
          persist();
        },
        request: (kind: string) => {
          result.calls[kind] = (result.calls[kind] ?? 0) + 1;
          persist();
        },
      };
      try {
        await runWithAiCallSink(
          (record) => {
            result.tokens.push(record);
            persist();
          },
          async () => {
            const signal = AbortSignal.timeout(1500000);
            await (job.version === 3 ? runReadingV3 : runReconstruction)(
              page,
              config,
              signal,
              hooks,
            );
            if (job.image) {
              const imageConfig = resolveImageGenConfig();
              if (!imageConfig) throw new Error("生图模型未配置");
              const asset = page.assets.find((a) => a.role === "generated");
              if (!asset) throw new Error("模型未规划图片");
              await generateThemeAsset(
                asset,
                page.designDirection ?? "",
                imageConfig,
                signal,
                checkpoint,
                () => {},
                () => hooks.request("imageCalls"),
              );
            }
            await compileReadingVisuals(page, signal, checkpoint, () => {});
            if (job.version === 3) {
              const fault = await probeReadingPage(page);
              if (fault) {
                for (const s of page.design.scripts ?? []) {
                  s.status = "failed";
                  s.error = fault;
                }
                page.warnings.push(fault);
              }
            }
            await fs.writeFile(
              path.join(directory, `${id}.html`),
              await exportThemedReadingHtml(page),
            );
            if (job.version === 3)
              await fs.writeFile(
                path.join(directory, `${id}-static.html`),
                await exportThemedReadingHtml(page, false, true),
              );
            if (input.anomalyLedger) {
              // 第十次先完成与短文基线相同的离线页面；随后只为同一页补图，不重新调用页面设计。
              result.coreElapsedMs = Date.now() - result.startedAt;
              result.coreCalls = { ...result.calls };
              result.coreComplete =
                page.warnings.length === 0 &&
                !page.design.scripts?.some((s) => s.status === "failed");
              checkpoint();
              page.options.generateImages = true;
              page.options.maxImages = 1;
              const plan = await planTheme(page, config, signal, () =>
                hooks.request("textCalls"),
              );
              const asset = plan.assets.find((a) => a.role === "generated");
              if (!asset) throw new Error("同页补图未规划有效图片");
              page.assets.push(asset);
              ensureRequestedReadingImage(page);
              checkpoint();
              const imageConfig = resolveImageGenConfig();
              if (!imageConfig) throw new Error("生图模型未配置");
              await generateThemeAsset(
                asset,
                page.designDirection ?? "",
                imageConfig,
                signal,
                checkpoint,
                () => {},
                () => hooks.request("imageCalls"),
              );
              result.imageSaved = asset.status === "ready";
              await fs.writeFile(
                path.join(directory, `${id}-with-image.html`),
                await exportThemedReadingHtml(page),
              );
              await fs.writeFile(
                path.join(directory, `${id}-with-image-static.html`),
                await exportThemedReadingHtml(page, false, true),
              );
            }
          },
        );
        result.status =
          page.warnings.length ||
          page.design?.scripts?.some((s) => s.status === "failed")
            ? "partial"
            : "completed";
        result.firstContentMs ??= Date.now() - result.startedAt;
      } catch (e) {
        result.status = "failed";
        result.error = e instanceof Error ? e.message : String(e);
      }
      result.elapsedMs = Date.now() - result.startedAt;
      result.repairs = page.generation?.repairs;
      checkpoint();
      if (/429|限流|频繁/.test(result.error ?? "")) break;
    }
    return attempts.map(({ tokens, ...rest }) => ({
      ...rest,
      promptTokens: tokens.reduce((n, r) => n + (r.promptTokens ?? 0), 0),
      completionTokens: tokens.reduce(
        (n, r) => n + (r.completionTokens ?? 0),
        0,
      ),
    }));
  } finally {
    coreAIConfigService.read = originalRead;
  }
}
