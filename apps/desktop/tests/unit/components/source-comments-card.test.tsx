import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeAll, beforeEach, expect, it, vi } from "vitest";
import type { KnowledgeItem, SourceComment } from "@guizhi/shared/types";
import { SourceCommentsProvider } from "../../../src/renderer/components/library/SourceCommentsContext";
import { SourceCommentsCard } from "../../../src/renderer/components/library/SourceCommentsCard";
import { ItemDetailHeader } from "../../../src/renderer/components/library/ItemDetailHeader";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";

const { showToast } = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock("../../../src/renderer/components/ui/Toast", () => ({
  useToast: () => ({ showToast }),
}));
const item: KnowledgeItem = {
  id: "one",
  title: "科普视频",
  content: "正文",
  itemType: "video",
  status: "active",
  isFavorite: false,
  isPinned: false,
  sourceUri: "https://www.douyin.com/video/123",
  createdAt: 1,
  updatedAt: 1,
  tags: [],
};
const comment: SourceComment = {
  id: "c1",
  itemId: "one",
  platform: "douyin",
  externalId: "c1",
  authorName: "读者",
  content: "<script>alert(1)</script> **纯文本**",
  likeCount: 3,
  publishedAt: null,
  capturedAt: 1,
};
function View({ entry = item }: { entry?: KnowledgeItem }) {
  return (
    <SourceCommentsProvider key={entry.id} item={entry}>
      <ItemDetailHeader item={entry} isTrashed={!!entry.deletedAt} />
      <SourceCommentsCard />
    </SourceCommentsProvider>
  );
}
beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});
beforeEach(() => {
  showToast.mockClear();
  installWindowMocks({
    api: {
      platformCapture: {
        listComments: vi.fn().mockResolvedValue([]),
        refreshComments: vi.fn().mockResolvedValue([comment]),
      },
    },
  });
});

it("空卡片隐藏，更多菜单打开配置后才采集；成功后保留评论", async () => {
  render(<View />);
  await waitFor(() =>
    expect(window.api.platformCapture.listComments).toHaveBeenCalledWith(
      item.id,
    ),
  );
  expect(screen.queryByRole("button", { name: /来源评论/ })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "更多操作" }));
  fireEvent.click(screen.getByText("采集评论", { exact: true }));
  expect(window.api.platformCapture.refreshComments).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "采集评论" }));
  await screen.findByText(comment.content);
  expect(window.api.platformCapture.refreshComments).toHaveBeenCalledWith({
    itemId: item.id,
    limit: 20,
  });
  fireEvent.click(screen.getByRole("button", { name: /来源评论/ }));
  expect(screen.getByRole("button", { name: /来源评论\s*1/ })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  expect(screen.queryByRole("button", { name: "刷新" })).toBeNull();
});

it("已有评论默认显示条数并折叠，展开后按纯文本展示", async () => {
  vi.mocked(window.api.platformCapture.listComments).mockResolvedValue([
    comment,
  ]);
  const { container } = render(<View />);
  const toggle = await screen.findByRole("button", { name: /来源评论\s*1/ });
  expect(screen.queryByText(comment.content)).toBeNull();
  expect(screen.queryByRole("button", { name: "评论采集数量" })).toBeNull();
  fireEvent.click(toggle);
  expect(screen.getByText(comment.content)).toBeInTheDocument();
  expect(container.querySelector("script")).toBeNull();
  expect(container.querySelector("strong")).toBeNull();
});

it("本地读取失败显示可重试错误，不自动访问平台", async () => {
  vi.mocked(window.api.platformCapture.listComments).mockRejectedValueOnce(
    new Error("读取评论失败"),
  );
  render(<View />);
  expect(await screen.findByRole("alert")).toHaveTextContent("读取评论失败");
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  expect(screen.queryByRole("button", { name: /来源评论/ })).toBeNull();
  expect(window.api.platformCapture.refreshComments).not.toHaveBeenCalled();
});

it("采集失败保留原因和可复制详情，重试空结果不冒充成功", async () => {
  vi.mocked(window.api.platformCapture.refreshComments)
    .mockRejectedValueOnce(new Error("登录已失效"))
    .mockResolvedValueOnce([]);
  render(<View />);
  await act(async () => {});
  fireEvent.click(screen.getByRole("button", { name: "更多操作" }));
  fireEvent.click(screen.getByText("采集评论", { exact: true }));
  fireEvent.click(screen.getByRole("button", { name: "采集评论" }));
  expect(await screen.findByText("登录已失效")).toBeInTheDocument();
  expect(showToast).toHaveBeenCalledWith("采集评论失败", "error", {
    detail: "登录已失效",
  });
  expect(screen.queryByText("还没有来源评论")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "采集评论" }));
  await screen.findByText("未取得评论，可稍后重试");
});

it.each([
  "https://linux.do/t/topic/123",
  "https://www.v2ex.com/t/123",
  "https://bbs.nga.cn/read.php?tid=123",
  "https://meta.appinn.net/t/topic/123",
  "https://2libra.com/post/test/1234567",
  "https://www.bilibili.com/video/BV123",
  "https://youtube.com/watch?v=123",
  "https://example.com",
  undefined,
])("不支持来源 %s 没有入口，也不读取独立评论", async (sourceUri) => {
  render(<View entry={{ ...item, sourceUri }} />);
  fireEvent.click(screen.getByRole("button", { name: "更多操作" }));
  expect(screen.queryByText("采集评论", { exact: true })).toBeNull();
  expect(window.api.platformCapture.listComments).not.toHaveBeenCalled();
});

it("切换条目后忽略上一条的迟到评论", async () => {
  let finish!: (value: SourceComment[]) => void;
  vi.mocked(window.api.platformCapture.listComments).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const view = render(<View />);
  view.rerender(<View entry={{ ...item, id: "two" }} />);
  await act(async () => finish([comment]));
  expect(screen.queryByRole("button", { name: /来源评论/ })).toBeNull();
  expect(screen.queryByText(comment.content)).toBeNull();
});
