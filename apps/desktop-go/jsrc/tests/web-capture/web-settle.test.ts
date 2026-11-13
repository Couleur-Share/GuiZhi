// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import {
  INITIAL_CAP_MS,
  LEGACY_SETTLE_MS,
  SCROLL_STEPS,
  STEP_CAP_MS,
  installSettleProbe,
  pageSettleEnv,
  readSettleProbe,
  settleRenderedPage,
  webSettleMode,
  type WebPageSample,
  type WebSettleEnv,
} from "../../services/web-capture/web-settle";

/** 页面时间线：以“安装探针的时刻”为 0 的虚拟毫秒。 */
interface Timeline {
  /** DOM 变化发生的时刻。 */
  mutations?: number[];
  /** 子请求的 [开始, 结束) 区间。 */
  requests?: Array<[number, number]>;
  /** 可见文字长度：从某时刻起变为多少，按时间升序。 */
  text?: Array<[number, number]>;
  /** 探针脚本读取一次耗费的毫秒数（模拟 executeJavaScript 往返）。 */
  sampleCost?: number;
  /** 页面侧单次读取自身的计算耗时，写入样本的 costMs；缺省则不带该字段（旧版探针）。 */
  pageCost?: number;
  scrollCost?: number;
  /** 每次滚动（以当时的虚拟时刻为参数）时，向时间线追加事件，模拟懒加载。 */
  onScroll?: (now: number, timeline: Timeline) => void;
  /** 第 n 次读取返回 null（观察器丢失）。 */
  lostAtSample?: number;
}

function simulate(timeline: Timeline = {}, overrides: Partial<WebSettleEnv> = {}) {
  let t = 0;
  let installedAt = 0;
  let samples = 0;
  const calls = { install: 0, sample: 0, scroll: 0, pauses: [] as number[] };
  const mutationsUpTo = (at: number) =>
    Math.max(
      installedAt,
      ...(timeline.mutations ?? []).filter((time) => time <= at),
    );
  const env: WebSettleEnv = {
    now: () => t,
    async pause(ms) {
      calls.pauses.push(ms);
      t += ms;
    },
    async scroll() {
      calls.scroll++;
      t += timeline.scrollCost ?? 0;
      timeline.onScroll?.(t, timeline);
    },
    networkQuietMs() {
      let last = -10_000;
      for (const [start, end] of timeline.requests ?? []) {
        if (start <= t && t < end) return 0;
        if (start <= t) last = Math.max(last, start);
        if (end <= t) last = Math.max(last, end);
      }
      return t - last;
    },
    async install() {
      calls.install++;
      installedAt = t;
      return true;
    },
    async sample(): Promise<WebPageSample | null> {
      calls.sample++;
      t += timeline.sampleCost ?? 0;
      samples++;
      if (samples === timeline.lostAtSample) return null;
      const text = (timeline.text ?? [[0, 5000]]).filter(
        ([from]) => from <= t,
      );
      return {
        domQuietMs: t - mutationsUpTo(t),
        textLength: text.at(-1)?.[1] ?? 0,
        ...(timeline.pageCost === undefined
          ? {}
          : { costMs: timeline.pageCost }),
      };
    },
    ...overrides,
  };
  return { env, calls, elapsed: () => t };
}

const signal = () => new AbortController().signal;

describe("事件驱动沉降等待", () => {
  it("各段上限之和等于旧版固定等待，保证最坏情况不变慢", () => {
    expect(INITIAL_CAP_MS).toBe(1200);
    expect(STEP_CAP_MS).toBe(200);
    expect(SCROLL_STEPS).toBe(3);
    expect(LEGACY_SETTLE_MS).toBe(1800);
  });

  it("静止页面在 300 ms 判定静止，三次滚动各只等 100 ms", async () => {
    const page = simulate();
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "quiet", initialMs: 300 });
    expect(page.calls.scroll).toBe(3);
    expect(page.elapsed()).toBe(600);
  });

  it("fixed 模式与旧行为一致：不碰页面，1200 ms 后三次（滚动 + 200 ms）", async () => {
    const page = simulate();
    const result = await settleRenderedPage(page.env, signal(), "fixed");
    expect(result).toMatchObject({ reason: "fixed", initialMs: 1200 });
    expect(page.calls).toMatchObject({ install: 0, sample: 0, scroll: 3 });
    expect(page.calls.pauses).toEqual([1200, 200, 200, 200]);
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("环境变量 GUIZHI_WEB_SETTLE=fixed 才关闭，其他取值与未设置都使用事件驱动", () => {
    const original = process.env.GUIZHI_WEB_SETTLE;
    try {
      delete process.env.GUIZHI_WEB_SETTLE;
      expect(webSettleMode()).toBe("quiet");
      process.env.GUIZHI_WEB_SETTLE = "fixed";
      expect(webSettleMode()).toBe("fixed");
      process.env.GUIZHI_WEB_SETTLE = "quiet";
      expect(webSettleMode()).toBe("quiet");
      process.env.GUIZHI_WEB_SETTLE = "off";
      expect(webSettleMode()).toBe("quiet");
    } finally {
      if (original === undefined) delete process.env.GUIZHI_WEB_SETTLE;
      else process.env.GUIZHI_WEB_SETTLE = original;
    }
  });

  it("子请求进行期间不提前结束，结束后再静止 300 ms 才退出", async () => {
    const page = simulate({ requests: [[100, 700]] });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "quiet", initialMs: 1000 });
  });

  it("相邻请求间隔小于静止窗口时被串成一条链，晚到的内容也能等到", async () => {
    // 链式请求：第一个在 200 ms 结束，150 ms 后发出第二个，间隔不足 300 ms，期间不判定静止。
    const page = simulate({
      requests: [
        [0, 200],
        [350, 550],
      ],
      mutations: [550],
      text: [
        [0, 3000],
        [550, 4000],
      ],
    });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result.reason).toBe("quiet");
    // 第二个请求在 550 ms 结束并写入 DOM，之后静止 300 ms 才退出（若只有第一个请求，500 ms 就会退出）。
    expect(result.initialMs).toBe(900);
  });

  it("已知限制：间隔超过静止窗口的链式请求会被漏掉（与旧版固定等待的差异）", async () => {
    const page = simulate({
      requests: [
        [0, 200],
        [600, 800],
      ],
      mutations: [800],
      text: [
        [0, 3000],
        [800, 4000],
      ],
    });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    // 200 ms 结束后静止满 300 ms 即在 500 ms 退出，第二个请求尚未发出。
    expect(result).toMatchObject({ reason: "quiet", initialMs: 500 });
  });

  it("DOM 持续变化（如动画）时等满首段，总耗时不超过旧版", async () => {
    const mutations = Array.from({ length: 200 }, (_, index) => index * 50);
    const page = simulate({ mutations });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "cap", initialMs: 1200 });
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("持续轮询的页面（请求永不间断）等满首段，总耗时不超过旧版", async () => {
    const requests = Array.from({ length: 100 }, (_, index): [number, number] => [
      index * 100,
      index * 100 + 30,
    ]);
    const page = simulate({ requests });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result.reason).toBe("cap");
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("记录各信号阻止提前结束的轮询次数与最后读数，便于解释为什么等满", async () => {
    // 一个迟迟不结束的请求：每次轮询都被“有未完成请求”拦住。
    const pending = simulate({ requests: [[0, 5000]] });
    const stuck = await settleRenderedPage(pending.env, signal(), "quiet");
    expect(stuck.reason).toBe("cap");
    // 0、100、…、1200 ms 共 13 次读取。
    expect(stuck.polls).toBe(13);
    expect(stuck.blocked).toEqual({ pending: 13, network: 0, dom: 3, text: 3, short: 0 });
    expect(stuck.last).toEqual({ networkQuietMs: 0, domQuietMs: 1200, textLength: 5000 });

    // DOM 每 50 ms 变化一次：被 dom 拦住，网络与文字都已静止。
    const mutations = Array.from({ length: 200 }, (_, index) => index * 50);
    const busy = simulate({ mutations });
    const animated = await settleRenderedPage(busy.env, signal(), "quiet");
    expect(animated.blocked).toMatchObject({ pending: 0, network: 0, dom: 13 });
    expect(animated.last!.domQuietMs).toBeLessThan(50);
  });

  it("环境提供在途明细时一并记入 last：inflight 计数与 blockers；没有阻止者时省略 blockers", async () => {
    const blockers = [{ kind: "other", origin: "https://api.example.com", ageMs: 1200 }];
    const stuck = simulate(
      { requests: [[0, 5000]] },
      { inflight: () => ({ other: 1, image: 2 }), blockers: () => blockers },
    );
    const capped = await settleRenderedPage(stuck.env, signal(), "quiet");
    expect(capped.last).toMatchObject({ inflight: { other: 1, image: 2 }, blockers });

    const settled = simulate({}, { inflight: () => ({ image: 1 }), blockers: () => [] });
    const quiet = await settleRenderedPage(settled.env, signal(), "quiet");
    expect(quiet.last).toMatchObject({ inflight: { image: 1 } });
    expect(quiet.last).not.toHaveProperty("blockers");
  });

  it("记录最长读取往返与页面侧计算耗时，区分“页面主线程忙”和“轮询本身太重”", async () => {
    // 往返 150 ms（含排队），页面侧只算了 12 ms：说明时间花在排队上而不是 innerText。
    const busy = simulate({ sampleCost: 150, pageCost: 12 });
    const queued = await settleRenderedPage(busy.env, signal(), "quiet");
    expect(queued).toMatchObject({ reason: "quiet", maxPollMs: 150, maxCostMs: 12 });

    // 探针没有回报 costMs（旧版探针）：计为 0，不影响其余判定。
    const legacy = simulate({ sampleCost: 5 });
    const plain = await settleRenderedPage(legacy.env, signal(), "quiet");
    expect(plain).toMatchObject({ reason: "quiet", maxPollMs: 5, maxCostMs: 0 });
  });

  it("没有读到页面状态（固定等待）时不带统计字段", async () => {
    const page = simulate();
    const result = await settleRenderedPage(page.env, signal(), "fixed");
    expect(result).toEqual({ reason: "fixed", initialMs: 1200 });
  });

  it("读取开销计入时限：探针往返变慢也不会超过旧版太多", async () => {
    const mutations = Array.from({ length: 200 }, (_, index) => index * 50);
    const page = simulate({ mutations, sampleCost: 4, scrollCost: 3 });
    await settleRenderedPage(page.env, signal(), "quiet");
    // 每段最多多出一次读取与一次滚动的往返。
    const tolerance = (1 + SCROLL_STEPS) * 4 + SCROLL_STEPS * 3;
    expect(page.elapsed()).toBeLessThanOrEqual(LEGACY_SETTLE_MS + tolerance);
  });

  it("空壳页（可见文字很少）保持完整等待，原因记为 shell", async () => {
    const page = simulate({ text: [[0, 40]] });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "shell", initialMs: 1200 });
  });

  it("空壳页静默渲染（无请求）也能等到：保护期内出现正文后再静止才退出", async () => {
    const page = simulate({
      mutations: [900],
      text: [
        [0, 40],
        [900, 5000],
      ],
    });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "quiet", initialMs: 1200 });
  });

  it("可见文字仍在变化（类名显隐、无 DOM 变化）时不判定静止", async () => {
    const page = simulate({
      text: [
        [0, 300],
        [250, 600],
        [450, 2400],
      ],
    });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    // 文字在 450 ms 最后一次变化，500 ms 的采样才看到；此后需连续 300 ms 不变才退出。
    // 若只比较相邻两次采样（相隔 100 ms），400 ms 就会在 600 字时误判静止。
    expect(result).toMatchObject({ reason: "quiet", initialMs: 800 });
  });

  it("滚动触发的懒加载请求：该步等待请求结束，但不超过 200 ms", async () => {
    const timeline: Timeline = {
      onScroll: (now, current) => {
        current.requests = [...(current.requests ?? []), [now + 20, now + 70]];
        current.mutations = [...(current.mutations ?? []), now + 70];
      },
    };
    const page = simulate(timeline);
    await settleRenderedPage(page.env, signal(), "quiet");
    const stepPauses = page.calls.pauses.slice(page.calls.pauses.length - 9);
    // 每步：100 ms 保底 + 50 ms 复查，请求刚结束（<50 ms）与 DOM 刚变化（<100 ms）都要再等，
    // 最终在 200 ms 上限处结束。
    expect(stepPauses.reduce((sum, ms) => sum + ms, 0)).toBeLessThanOrEqual(
      SCROLL_STEPS * STEP_CAP_MS,
    );
    expect(page.elapsed()).toBe(300 + SCROLL_STEPS * STEP_CAP_MS);
  });

  it("滚动没有触发任何活动时，每步在 100 ms 保底后立即继续", async () => {
    const page = simulate();
    await settleRenderedPage(page.env, signal(), "quiet");
    expect(page.calls.pauses.slice(-3)).toEqual([100, 100, 100]);
  });

  it("观察器丢失（读取返回 null）时重新安装，并在之后恢复判定", async () => {
    const page = simulate({ lostAtSample: 1 });
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(page.calls.install).toBe(2);
    expect(result.reason).toBe("quiet");
  });

  it("重新安装失败时退回固定等待，并记为 blind", async () => {
    const page = simulate({ lostAtSample: 1 });
    let installs = 0;
    page.env.install = async () => ++installs === 1;
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result.reason).toBe("blind");
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("安装探针失败时退回固定等待：时长与旧版一致", async () => {
    const page = simulate();
    page.env.install = async () => false;
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "blind", initialMs: 1200 });
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("安装探针在页面主线程排队的时间也算进首段上限：页面刚加载完忙于水合时不会比旧版更久", async () => {
    // 真实站点（Next.js 等）加载完立刻水合数百毫秒，executeJavaScript 要排在后面；
    // 此前上限从安装完成后才开始计时，首段因此多出这段排队（实测 +0.3 到 +0.4 s）。
    const page = simulate({ requests: [[0, 5000]] });
    const install = page.env.install;
    page.env.install = async () => {
      await page.env.pause(400);
      return install();
    };
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "cap", initialMs: 1200 });
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("安装探针排队后失败：退回固定等待只补足剩余时间，不再重复等满", async () => {
    const page = simulate();
    page.env.install = async () => {
      await page.env.pause(300);
      return false;
    };
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result).toMatchObject({ reason: "blind", initialMs: 1200 });
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("安装探针抛出页面侧错误时同样退回固定等待", async () => {
    const page = simulate();
    page.env.install = async () => {
      throw new Error("Script failed to execute");
    };
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result.reason).toBe("blind");
    expect(page.calls.scroll).toBe(3);
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("首段读取失败：补足剩余时间后退回固定等待，不重复等待", async () => {
    const page = simulate();
    let reads = 0;
    const original = page.env.sample;
    page.env.sample = async () => {
      if (++reads === 3) throw new Error("Object has been destroyed");
      return original();
    };
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result.reason).toBe("blind");
    // 已经等了 200 ms，只补足剩余的 1000 ms，总时长仍是旧版的 1200 ms 首段。
    expect(result.initialMs).toBe(1200);
    expect(page.elapsed()).toBe(LEGACY_SETTLE_MS);
  });

  it("滚动阶段读取失败：该步补足上限并退回固定等待", async () => {
    const page = simulate();
    let reads = 0;
    const original = page.env.sample;
    page.env.sample = async () => {
      // 首段需要 4 次读取（0/100/200/300 ms），之后第一次读取发生在第一步的滚动阶段。
      if (++reads === 5) throw new Error("Object has been destroyed");
      return original();
    };
    const result = await settleRenderedPage(page.env, signal(), "quiet");
    expect(result.reason).toBe("quiet");
    expect(page.elapsed()).toBe(300 + SCROLL_STEPS * STEP_CAP_MS);
  });

  it("取消时读取失败要向上传播，不当作页面失败吞掉", async () => {
    const controller = new AbortController();
    const page = simulate();
    page.env.sample = async () => {
      controller.abort();
      throw new Error("网页采集已取消");
    };
    await expect(
      settleRenderedPage(page.env, controller.signal, "quiet"),
    ).rejects.toThrow("取消");
  });

  it("取消时安装探针失败同样向上传播", async () => {
    const controller = new AbortController();
    const page = simulate();
    page.env.install = async () => {
      controller.abort();
      throw new Error("网页采集已取消");
    };
    await expect(
      settleRenderedPage(page.env, controller.signal, "quiet"),
    ).rejects.toThrow("取消");
  });

  it("滚动失败直接抛出，与旧版一致", async () => {
    const page = simulate();
    page.env.scroll = async () => {
      throw new Error("Object has been destroyed");
    };
    await expect(settleRenderedPage(page.env, signal(), "quiet")).rejects.toThrow(
      "destroyed",
    );
  });

  it("时钟停滞时也会终止（兜底轮数），并按空壳/封顶归类", async () => {
    let polls = 0;
    const frozen: WebSettleEnv = {
      now: () => 0,
      pause: async () => undefined,
      scroll: async () => undefined,
      networkQuietMs: () => 0,
      install: async () => true,
      sample: async () => {
        polls++;
        return { domQuietMs: 0, textLength: 5000 };
      },
    };
    const result = await settleRenderedPage(frozen, signal(), "quiet");
    expect(result.reason).toBe("cap");
    expect(polls).toBeLessThanOrEqual(14 + SCROLL_STEPS * 6);
  });
});

describe("页面侧探针", () => {
  // jsdom 不实现 innerText；这里按 textContent 近似，只验证探针的流程与自包含性。
  function installInnerText() {
    Object.defineProperty(document.body, "innerText", {
      configurable: true,
      get: () => document.body.textContent ?? "",
    });
  }
  const evaluate = async (script: string) => (0, eval)(script);

  it("脚本序列化后自包含：可在全局作用域直接求值", async () => {
    installInnerText();
    document.body.innerHTML = "<p>hello</p>";
    const env = pageSettleEnv({
      evaluate,
      pause: async () => undefined,
      networkQuietMs: () => 0,
    });
    expect(await env.install()).toBe(true);
    const sample = await env.sample();
    expect(sample?.textLength).toBe(5);
    expect(sample?.domQuietMs).toBeGreaterThanOrEqual(0);
    expect(sample?.costMs).toBeGreaterThanOrEqual(0);
  });

  it("DOM 变化后静止时长归零，随时间增长；重复安装不叠加观察器", async () => {
    installInnerText();
    document.body.innerHTML = "<p>a</p>";
    expect(installSettleProbe()).toBe(true);
    expect(installSettleProbe()).toBe(true);
    await new Promise((resolve) => setTimeout(resolve, 30));
    const quiet = readSettleProbe();
    expect(quiet?.domQuietMs).toBeGreaterThanOrEqual(25);
    document.body.append(document.createElement("div"));
    await Promise.resolve();
    await new Promise((resolve) => setTimeout(resolve, 0));
    const changed = readSettleProbe();
    expect(changed!.domQuietMs).toBeLessThan(quiet!.domQuietMs);
  });

  it("文字变化也算 DOM 活动，探针不污染常规属性枚举", async () => {
    installInnerText();
    document.body.innerHTML = "<p id='x'>a</p>";
    installSettleProbe();
    expect(Object.keys(window)).not.toContain("guizhi.settle");
    expect(Object.getOwnPropertySymbols(window).length).toBeGreaterThan(0);
    await new Promise((resolve) => setTimeout(resolve, 30));
    const before = readSettleProbe()!.domQuietMs;
    document.getElementById("x")!.firstChild!.textContent = "bb";
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(readSettleProbe()!.domQuietMs).toBeLessThan(before);
  });

  it("探针不存在（文档被替换）时读取返回 null", () => {
    const key = Symbol.for("guizhi.settle");
    Reflect.deleteProperty(window, key);
    expect(readSettleProbe()).toBeNull();
  });

  it("页面返回无法识别的数据时按读取失败处理", async () => {
    const env = pageSettleEnv({
      evaluate: async () => ({ html: "<p>快照</p>" }),
      pause: async () => undefined,
      networkQuietMs: () => 0,
    });
    await expect(env.sample()).rejects.toThrow("无法识别");
    expect(await env.install()).toBe(false);
  });

  it("costMs 只在缺省或为有限数字时接受，其余同样按读取失败处理", async () => {
    const sampleOf = (value: unknown) =>
      pageSettleEnv({
        evaluate: async () => value,
        pause: async () => undefined,
        networkQuietMs: () => 0,
      }).sample();
    await expect(sampleOf({ domQuietMs: 1, textLength: 2 })).resolves.toEqual({
      domQuietMs: 1,
      textLength: 2,
    });
    await expect(
      sampleOf({ domQuietMs: 1, textLength: 2, costMs: 3 }),
    ).resolves.toMatchObject({ costMs: 3 });
    for (const costMs of [Number.NaN, Infinity, "3", null]) {
      await expect(
        sampleOf({ domQuietMs: 1, textLength: 2, costMs }),
      ).rejects.toThrow("无法识别");
    }
  });
});
