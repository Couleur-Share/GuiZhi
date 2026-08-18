import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { CaptureDialog } from "../../../src/renderer/components/capture/CaptureDialog";
import { DISCOVERY_DRAFT_KEY } from "../../../src/renderer/components/imports/platform-discovery-draft";

/** 抖音「复制打开抖音」口令原样，尾部那串是分享校验噪音 */
const DOUYIN_SHARE_TEXT =
  "0.02 复制打开抖音，看看【六叔ultra的作品】4 个提示词，让 AI 像博士一样帮你调研🔥 用A... https://v.douyin.com/0lZNY93J6Ck/ :3pm t@E.Ul FhB:/ 12/15";
const DOUYIN_SHORT_URL = "https://v.douyin.com/0lZNY93J6Ck/";

let enqueue: ReturnType<typeof vi.fn>;

async function pasteDraft(text: string) {
  const user = userEvent.setup();
  render(
    <ToastProvider>
      <CaptureDialog isOpen onClose={() => {}} />
    </ToastProvider>,
  );
  await user.click(screen.getByTestId("capture-draft"));
  await user.paste(text);
  return user;
}

/**
 * 分享口令粘进快速采集框：链接得被抠出来送进采集队列。
 * 此前整段（含中文说明与校验噪音）会被存成一条文本笔记，视频一条也没采到。
 */
describe("快速采集识别分享口令", () => {
  beforeAll(async () => {
    await i18nReady;
    await changeLanguage("zh");
  });

  beforeEach(() => {
    sessionStorage.clear();
    enqueue = vi.fn().mockResolvedValue([]);
    installWindowMocks({
      api: {
        collection: { list: vi.fn().mockResolvedValue([]) },
        import: {
          enqueue,
          list: vi.fn().mockResolvedValue([]),
          selectFiles: vi.fn().mockResolvedValue([]),
        },
      },
    });
  });

  it("抖音口令默认采集其中的短链，并把链接显示出来", async () => {
    const user = await pasteDraft(DOUYIN_SHARE_TEXT);

    expect(
      screen.getByText(`已提取链接：${DOUYIN_SHORT_URL}`),
    ).toBeInTheDocument();

    await user.click(screen.getByTestId("capture-submit"));
    expect(enqueue).toHaveBeenCalledWith([
      {
        kind: "url",
        input: DOUYIN_SHORT_URL,
        collectionId: null,
        tagNames: undefined,
      },
    ]);
  });

  it("改判后整段口令按文本笔记存", async () => {
    const user = await pasteDraft(DOUYIN_SHARE_TEXT);

    await user.click(screen.getByTestId("capture-switch-kind"));
    expect(screen.getByText("将保存为文本笔记")).toBeInTheDocument();

    await user.click(screen.getByTestId("capture-submit"));
    expect(enqueue).toHaveBeenCalledWith([
      {
        kind: "text",
        input: DOUYIN_SHARE_TEXT,
        collectionId: null,
        tagNames: undefined,
      },
    ]);
  });

  it("普通网页链接夹在自己写的文字里，默认仍存文本", async () => {
    const user = await pasteDraft("明天看这个 https://example.com/a");

    expect(screen.getByText("将保存为文本笔记")).toBeInTheDocument();

    await user.click(screen.getByTestId("capture-switch-kind"));
    await user.click(screen.getByTestId("capture-submit"));
    expect(enqueue).toHaveBeenCalledWith([
      {
        kind: "url",
        input: "https://example.com/a",
        collectionId: null,
        tagNames: undefined,
      },
    ]);
  });

  it("单个作者主页把主操作改为浏览作品，并保留普通网页导入", async () => {
    const user = await pasteDraft("https://www.xiaohongshu.com/user/profile/abc");
    expect(screen.getByRole("button", { name: "浏览作者作品" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "按普通网页导入" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "浏览作者作品" }));
    expect(JSON.parse(sessionStorage.getItem(DISCOVERY_DRAFT_KEY)!)).toMatchObject({
      url: "https://www.xiaohongshu.com/user/profile/abc",
    });
    expect(enqueue).not.toHaveBeenCalled();
  });
});
