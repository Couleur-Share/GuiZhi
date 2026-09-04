import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, describe, expect, it, vi } from "vitest";

const { createElectronCaptureContextMock, clearElectronCaptureSessionsMock } =
  vi.hoisted(() => ({
    createElectronCaptureContextMock: vi.fn(),
    clearElectronCaptureSessionsMock: vi.fn().mockResolvedValue(undefined),
  }));

vi.mock(
  "../../../src/main/services/platform-capture/electron-capture-runtime",
  () => ({
    createElectronCaptureContext: createElectronCaptureContextMock,
    clearElectronCaptureSessions: clearElectronCaptureSessionsMock,
  }),
);
import {
  BrowserCaptureService,
  didLoginCookieSnapshotChange,
  isAllowedBrowserResourceUrl,
  scanPlatformComments,
  scanPlatformDiscoveryPayloads,
  shouldBlockLoginPageRequest,
} from "../../../src/main/services/platform-capture/browser-capture";
import {
  detectPlatformCapturePlatform,
  detectPlatformCreatorUrl,
  isAllowedPlatformUrl,
} from "@guizhi/shared/utils/platform-capture";

const temporary: string[] = [];

function tempDir(): string {
  const dir = fs.mkdtempSync(
    path.join(os.tmpdir(), "guizhi-platform-capture-"),
  );
  temporary.push(dir);
  return dir;
}

afterEach(() => {
  createElectronCaptureContextMock.mockReset();
  clearElectronCaptureSessionsMock.mockClear();
  for (const dir of temporary.splice(0)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

describe("平台登录态采集边界", () => {
  it("只接受官方 HTTPS 域名，拒绝相似后缀与非法协议", () => {
    expect(
      isAllowedPlatformUrl(
        "xiaohongshu",
        "https://www.xiaohongshu.com/explore/abc",
      ),
    ).toBe(true);
    expect(
      isAllowedPlatformUrl(
        "xiaohongshu",
        "https://xiaohongshu.com.evil.test/explore/abc",
      ),
    ).toBe(false);
    expect(isAllowedPlatformUrl("douyin", "https://v.douyin.com/abc/")).toBe(
      true,
    );
    expect(
      isAllowedPlatformUrl("douyin", "http://www.douyin.com/video/1"),
    ).toBe(false);
    expect(detectPlatformCapturePlatform("javascript:alert(1)")).toBeNull();
  });

  it("放行抖音页面必需资源，同时拒绝遥测和相似恶意后缀", () => {
    for (const url of [
      "https://lf-douyin-pc-web.douyinstatic.com/obj/app.css",
      "https://sf1-cdn-tos.douyinstatic.com/obj/app.js",
      "https://lf-c-flwb.bytetos.com/obj/font.woff2",
      "https://lf-cdn-tos.bytescm.com/obj/image.webp",
      "https://lf-security.bytegoofy.com/goofy/secure.js",
      "https://lf-ucenter-web.yhgfb-cn-static.com/obj/login.js",
    ]) {
      expect(isAllowedBrowserResourceUrl("douyin", url)).toBe(true);
    }

    for (const url of [
      "http://lf-douyin-pc-web.douyinstatic.com/app.css",
      "https://douyinstatic.com.evil.test/app.js",
      "https://notdouyinstatic.com/app.js",
      "https://lf-static.applogcdn.com/log.js",
      "https://lf3-short.ibytedapm.com/monitor.js",
      "https://lf-rc1.yhgfb-cn-static.com/unknown.js",
    ]) {
      expect(isAllowedBrowserResourceUrl("douyin", url)).toBe(false);
    }
  });

  it("登录页阻断推荐流和音视频，但保留二维码与验证码图片", () => {
    expect(
      shouldBlockLoginPageRequest(
        "douyin",
        "https://www.douyin.com/aweme/v1/web/tab/feed/?count=10",
        "xhr",
      ),
    ).toBe(true);
    expect(
      shouldBlockLoginPageRequest(
        "douyin",
        "https://www.douyin.com/aweme/v1/web/follow/feed/?count=10",
        "fetch",
      ),
    ).toBe(true);
    expect(
      shouldBlockLoginPageRequest(
        "douyin",
        "https://v3.douyinvod.com/video/tos/example",
        "media",
      ),
    ).toBe(true);
    expect(
      shouldBlockLoginPageRequest(
        "douyin",
        "https://p3-sign.douyinpic.com/tos-cn-i-0813/example.jpeg",
        "image",
      ),
    ).toBe(true);
    expect(
      shouldBlockLoginPageRequest(
        "douyin",
        "https://www.douyin.com/passport/web/login/qrcode/",
        "xhr",
      ),
    ).toBe(false);
    expect(
      shouldBlockLoginPageRequest(
        "douyin",
        "https://p3-passport-byteacctimg.com/img/example.png",
        "image",
      ),
    ).toBe(false);
  });

  it("小红书访客会话存在时不算登录，只有登录 Cookie 相对基线变化才算", () => {
    const guest = { web_session: "guest-session" };
    expect(didLoginCookieSnapshotChange(guest, guest)).toBe(false);
    expect(
      didLoginCookieSnapshotChange(guest, {
        web_session: "authenticated-session",
      }),
    ).toBe(true);
    expect(
      didLoginCookieSnapshotChange(guest, {
        ...guest,
        web_session_v2: "authenticated-v2",
      }),
    ).toBe(true);
  });

  it("准确识别单个作者主页，而不是作品页或混合文本", () => {
    expect(
      detectPlatformCreatorUrl("https://www.xiaohongshu.com/user/profile/abc")
        ?.platform,
    ).toBe("xiaohongshu");
    expect(
      detectPlatformCreatorUrl("https://www.douyin.com/user/MS4wLjABAAAA")
        ?.platform,
    ).toBe("douyin");
    expect(
      detectPlatformCreatorUrl("https://www.douyin.com/video/123"),
    ).toBeNull();
    expect(
      detectPlatformCreatorUrl("看这里 https://www.douyin.com/user/abc"),
    ).toBeNull();
  });

  it("平台登录始终使用归知内置 Chromium 会话", () => {
    const service = new BrowserCaptureService({ userDataPath: tempDir() });
    expect(service.getStatuses()).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ browser: "embedded", available: true }),
      ]),
    );
  });

  it("专用档案严格落在 userData/browser-capture 下", () => {
    const userData = tempDir();
    const service = new BrowserCaptureService({ userDataPath: userData });
    expect(service.getProfileDir()).toBe(
      path.join(userData, "browser-capture", "electron-session"),
    );
  });

  it("从外部浏览器档案升级后要求全部平台重新登录", () => {
    const root = tempDir();
    const chrome = path.join(root, "Google/Chrome/Application/chrome.exe");
    fs.mkdirSync(path.dirname(chrome), { recursive: true });
    fs.writeFileSync(chrome, "");
    const userData = path.join(root, "user-data");
    const stateDir = path.join(userData, "browser-capture");
    fs.mkdirSync(stateDir, { recursive: true });
    fs.writeFileSync(
      path.join(stateDir, "session-status.json"),
      JSON.stringify({ xiaohongshu: true, douyin: true }),
    );

    const service = new BrowserCaptureService({
      userDataPath: userData,
      platform: "win32",
      env: { PROGRAMFILES: root, "PROGRAMFILES(X86)": "", LOCALAPPDATA: "" },
    });
    const statuses = service.getStatuses();

    expect(
      statuses.find((entry) => entry.platform === "xiaohongshu")?.loggedIn,
    ).toBe(false);
    expect(
      statuses.find((entry) => entry.platform === "douyin")?.loggedIn,
    ).toBe(false);
    expect(
      statuses.find((entry) => entry.platform === "linuxdo")?.loggedIn,
    ).toBe(false);
  });

  it("抖音内置登录窗优先使用创作者中心快速入口", async () => {
    const root = tempDir();

    let loggedIn = false;
    const loginTarget = {
      isVisible: vi.fn().mockResolvedValue(true),
      waitFor: vi.fn().mockResolvedValue(undefined),
      click: vi.fn().mockImplementation(async () => {
        loggedIn = true;
      }),
    };
    const panelLocator = {
      first: vi.fn(),
      isVisible: vi.fn().mockImplementation(async () => loggedIn),
      waitFor: vi.fn().mockImplementation(async () => {
        if (!loggedIn) throw new Error("登录面板尚未出现");
      }),
    };
    panelLocator.first.mockReturnValue(panelLocator);
    const emptyLocator = {
      first: vi.fn(),
      waitFor: vi.fn().mockRejectedValue(new Error("未找到")),
      click: vi.fn(),
    };
    emptyLocator.first.mockReturnValue(emptyLocator);
    const loginLocator = { first: vi.fn().mockReturnValue(loginTarget) };
    const fastSurfaceLocator = {
      first: vi.fn(),
      waitFor: vi.fn().mockResolvedValue(undefined),
    };
    fastSurfaceLocator.first.mockReturnValue(fastSurfaceLocator);
    const page = {
      setDefaultNavigationTimeout: vi.fn(),
      setDefaultTimeout: vi.fn(),
      goto: vi.fn().mockResolvedValue(undefined),
      reload: vi.fn().mockResolvedValue(undefined),
      content: vi.fn().mockResolvedValue(""),
      isClosed: vi.fn().mockReturnValue(false),
      waitForTimeout: vi.fn().mockImplementation(async () => {
        loggedIn = true;
      }),
      evaluate: vi.fn().mockResolvedValue(false),
      scrollBy: vi.fn().mockResolvedValue(undefined),
      startJsonCapture: vi.fn().mockReturnValue([]),
      close: vi.fn().mockResolvedValue(undefined),
      locator: vi.fn().mockImplementation((selector: string) => {
        if (selector.includes("login-panel")) return panelLocator;
        if (selector === 'p:text-is("登录")') return loginLocator;
        return emptyLocator;
      }),
      getByRole: vi.fn().mockReturnValue(emptyLocator),
      getByText: vi
        .fn()
        .mockImplementation((text: string) =>
          text === "扫码登录" ? fastSurfaceLocator : emptyLocator,
        ),
    };
    const context = {
      page,
      browserVersion: "126.0.0",
      cookies: vi.fn().mockImplementation(async () =>
        loggedIn
          ? [
              {
                name: "LOGIN_STATUS",
                value: "1",
                domain: ".douyin.com",
                path: "/",
              },
            ]
          : [],
      ),
      clearStorageData: vi.fn().mockResolvedValue(undefined),
      close: vi.fn().mockResolvedValue(undefined),
    };
    createElectronCaptureContextMock.mockResolvedValue(context);

    const service = new BrowserCaptureService({
      userDataPath: path.join(root, "user-data"),
    });
    const status = await service.login("douyin", true);

    expect(status.loggedIn).toBe(true);
    expect(context.clearStorageData).toHaveBeenCalledTimes(1);
    expect(loginTarget.click).not.toHaveBeenCalled();
    expect(page.goto).toHaveBeenCalledWith(
      "https://creator.douyin.com/creator-micro/interactive/comment",
      expect.objectContaining({ waitUntil: "domcontentloaded" }),
    );
    expect(page.goto).toHaveBeenCalledTimes(1);
    expect(page.reload).not.toHaveBeenCalled();
    expect(createElectronCaptureContextMock).toHaveBeenCalledWith(
      expect.objectContaining({ platform: "douyin", visible: true }),
    );
  });

  it("小红书重新登录只导航一次，不刷新已经出现的官方登录框", async () => {
    const root = tempDir();

    let loggedIn = false;
    const visibleLocator = {
      first: vi.fn(),
      isVisible: vi.fn().mockResolvedValue(true),
      waitFor: vi.fn().mockResolvedValue(undefined),
      click: vi.fn().mockResolvedValue(undefined),
    };
    visibleLocator.first.mockReturnValue(visibleLocator);
    const emptyLocator = {
      first: vi.fn(),
      isVisible: vi.fn().mockResolvedValue(false),
      waitFor: vi.fn().mockRejectedValue(new Error("未找到")),
      click: vi.fn().mockResolvedValue(undefined),
    };
    emptyLocator.first.mockReturnValue(emptyLocator);
    const page = {
      setDefaultNavigationTimeout: vi.fn(),
      setDefaultTimeout: vi.fn(),
      goto: vi.fn().mockResolvedValue(undefined),
      reload: vi.fn().mockResolvedValue(undefined),
      content: vi.fn().mockResolvedValue(""),
      isClosed: vi.fn().mockReturnValue(false),
      waitForTimeout: vi.fn().mockImplementation(async () => {
        loggedIn = true;
      }),
      evaluate: vi.fn().mockResolvedValue(false),
      scrollBy: vi.fn().mockResolvedValue(undefined),
      startJsonCapture: vi.fn().mockReturnValue([]),
      close: vi.fn().mockResolvedValue(undefined),
      locator: vi
        .fn()
        .mockImplementation((selector: string) =>
          selector.includes("login-container") ||
          selector.includes("qrcode-img")
            ? visibleLocator
            : emptyLocator,
        ),
      getByRole: vi.fn().mockReturnValue(emptyLocator),
      getByText: vi.fn().mockReturnValue(emptyLocator),
    };
    const context = {
      page,
      browserVersion: "126.0.0",
      cookies: vi.fn().mockImplementation(async () =>
        loggedIn
          ? [
              {
                name: "web_session",
                value: "member-session",
                domain: ".xiaohongshu.com",
                path: "/",
              },
            ]
          : [],
      ),
      clearStorageData: vi.fn().mockResolvedValue(undefined),
      close: vi.fn().mockResolvedValue(undefined),
    };
    createElectronCaptureContextMock.mockResolvedValue(context);

    const service = new BrowserCaptureService({
      userDataPath: path.join(root, "user-data"),
    });
    const status = await service.login("xiaohongshu", true);

    expect(status.loggedIn).toBe(true);
    expect(page.goto).toHaveBeenCalledTimes(1);
    expect(page.goto).toHaveBeenCalledWith(
      "https://www.xiaohongshu.com/explore",
      expect.objectContaining({ waitUntil: "domcontentloaded" }),
    );
    expect(page.reload).not.toHaveBeenCalled();
    expect(context.clearStorageData).toHaveBeenCalledTimes(1);
    expect(createElectronCaptureContextMock).toHaveBeenCalledWith(
      expect.objectContaining({ platform: "xiaohongshu", visible: true }),
    );
  });

  it("研究取消信号会关闭正在导航的隐藏平台窗口", async () => {
    const userData = tempDir();
    const stateDir = path.join(userData, "browser-capture");
    fs.mkdirSync(stateDir, { recursive: true });
    fs.writeFileSync(
      path.join(stateDir, "session-status.json"),
      JSON.stringify({ version: 3, xiaohongshu: true }),
    );

    let rejectNavigation: ((error: Error) => void) | undefined;
    const page = {
      setDefaultNavigationTimeout: vi.fn(),
      setDefaultTimeout: vi.fn(),
      goto: vi.fn(
        () =>
          new Promise<void>((_resolve, reject) => {
            rejectNavigation = reject;
          }),
      ),
      reload: vi.fn(),
      url: vi.fn(() => "https://www.xiaohongshu.com/search_result"),
      content: vi.fn(),
      isClosed: vi.fn().mockReturnValue(false),
      waitForTimeout: vi.fn(),
      evaluate: vi.fn(),
      scrollBy: vi.fn(),
      startJsonCapture: vi.fn().mockReturnValue([]),
      close: vi.fn(),
      locator: vi.fn(),
      getByRole: vi.fn(),
      getByText: vi.fn(),
    };
    const context = {
      page,
      browserVersion: "126.0.0",
      cookies: vi.fn().mockResolvedValue([
        {
          name: "web_session",
          value: "member-session",
          domain: ".xiaohongshu.com",
          path: "/",
        },
      ]),
      clearStorageData: vi.fn(),
      close: vi.fn().mockImplementation(async () => {
        rejectNavigation?.(new Error("browser has been closed"));
      }),
    };
    createElectronCaptureContextMock.mockResolvedValue(context);
    const service = new BrowserCaptureService({ userDataPath: userData });
    const controller = new AbortController();
    const pending = service.search(
      { platform: "xiaohongshu", keyword: "本地知识库", limit: 20 },
      controller.signal,
    );
    await vi.waitFor(() => expect(page.goto).toHaveBeenCalled());

    controller.abort();

    await expect(pending).rejects.toMatchObject({ code: "canceled" });
    expect(context.close).toHaveBeenCalled();
  });

  it("从脱敏平台响应提取作品、游标卡片和热门评论", () => {
    const douyin = scanPlatformDiscoveryPayloads("douyin", [
      {
        aweme_list: [
          {
            aweme_id: "739001",
            desc: "测试视频",
            author: { nickname: "作者甲" },
            video: { cover: { url_list: ["https://img.example/cover.jpg"] } },
            create_time: 1_720_000_000,
            statistics: { digg_count: 88, comment_count: 9 },
          },
        ],
      },
    ]);
    expect(douyin[0]).toMatchObject({
      externalId: "739001",
      title: "测试视频",
      author: "作者甲",
      mediaType: "video",
      snippet: "测试视频",
      engagement: { likes: 88, comments: 9 },
      dateConfidence: "high",
      discoveryMethod: "captured-json",
    });

    const xhs = scanPlatformDiscoveryPayloads("xiaohongshu", [
      {
        items: [
          {
            note_id: "xhs-1",
            display_title: "图文笔记",
            image_list: [{ url_default: "https://img.example/a.jpg" }],
          },
        ],
      },
    ]);
    expect(xhs[0]).toMatchObject({ externalId: "xhs-1", mediaType: "image" });

    const comments = scanPlatformComments(
      [
        {
          comments: [
            {
              comment_id: "c1",
              content: "纯文本评论",
              user: { nickname: "读者" },
              like_count: 42,
            },
            { comment_id: "c2", content: "第二条", like_count: 3 },
            {
              comment_id: "c3",
              content: "二级回复",
              parent_comment_id: "c1",
              like_count: 99,
            },
          ],
        },
      ],
      1,
    );
    expect(comments).toEqual([
      {
        externalId: "c1",
        authorName: "读者",
        content: "纯文本评论",
        likeCount: 42,
        publishedAt: null,
      },
    ]);
  });
});
