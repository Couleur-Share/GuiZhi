import { describe, expect, it } from "vitest";

import {
  runExclusiveLocalTranscription,
  summarizeProcessLog,
} from "../../../src/main/services/media/funasr-service";

/**
 * 实测抓到的一段服务日志尾巴：全是 funasr 用 tqdm 原地刷新的推理进度条。
 * 用户当时看到的报错正文就是这一串，真正的原因早被挤出 300 字的窗口了。
 */
const PROGRESS_ONLY =
  " 0.043: 100%|\u001b[34m██████████\u001b[0m| 1/1 [00:01<00:00,  1.28s/it]" +
  "                    \u001b[A\rrtf_avg: 0.043: 100%|\u001b[34m██████████\u001b[0m|" +
  " 1/1 [00:01<00:00,  1.29s/it]\r\n\r\n\r  0%|\u001b[34m          \u001b[0m|" +
  " 0/1 [00:00<?, ?it/s]\u001b[A";

describe("summarizeProcessLog", () => {
  it("整段都是进度条时回空串，由调用方换成一句能看的兜底", () => {
    expect(summarizeProcessLog(PROGRESS_ONLY)).toBe("");
  });

  it("进度条淹没真实报错时，留下的是报错而不是进度条", () => {
    const log =
      "[guizhi-asr] loading iic/SenseVoiceSmall on cpu (diarize=False)\n" +
      "ERROR:    [Errno 10048] error while attempting to bind on address " +
      "('127.0.0.1', 8620): 通常每个套接字地址只允许使用一次\n" +
      PROGRESS_ONLY;

    const summary = summarizeProcessLog(log);
    expect(summary).toContain("10048");
    expect(summary).not.toContain("rtf_avg");
    expect(summary).not.toContain("it/s");
  });

  it("剥掉 ANSI 转义，只留终端上真正看得见的那一段", () => {
    expect(
      summarizeProcessLog("\u001b[31mModuleNotFoundError: funasr\u001b[0m"),
    ).toBe("ModuleNotFoundError: funasr");
  });

  it("`\\r` 原地覆盖的行只取最后一次写入的内容", () => {
    expect(summarizeProcessLog("正在下载模型\rTraceback (most recent call last)")).toBe(
      "Traceback (most recent call last)",
    );
  });

  it("超长时从尾部截断——底部才是崩溃点", () => {
    const log = `${"a".repeat(500)}\nTraceback: boom`;
    const summary = summarizeProcessLog(log, 60);
    expect(summary.startsWith("…")).toBe(true);
    expect(summary.endsWith("Traceback: boom")).toBe(true);
    expect(summary.length).toBe(61);
  });
});

describe("runExclusiveLocalTranscription", () => {
  it("同一时刻只放行一条：内置引擎本来就一次只做一条", async () => {
    let running = 0;
    let peak = 0;
    const task = async (value: number) => {
      running += 1;
      peak = Math.max(peak, running);
      await new Promise((resolve) => setTimeout(resolve, 10));
      running -= 1;
      return value;
    };

    const results = await Promise.all([
      runExclusiveLocalTranscription(() => task(1)),
      runExclusiveLocalTranscription(() => task(2)),
      runExclusiveLocalTranscription(() => task(3)),
    ]);

    expect(peak).toBe(1);
    expect(results).toEqual([1, 2, 3]);
  });

  it("前一条失败不该卡住后一条——链条只用来定序", async () => {
    const failed = runExclusiveLocalTranscription(() =>
      Promise.reject(new Error("转写请求失败")),
    );
    await expect(failed).rejects.toThrow("转写请求失败");
    await expect(
      runExclusiveLocalTranscription(() => Promise.resolve("ok")),
    ).resolves.toBe("ok");
  });
});
