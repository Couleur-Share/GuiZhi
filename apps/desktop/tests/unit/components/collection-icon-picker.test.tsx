import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { CollectionIconPicker } from "../../../src/renderer/components/library/CollectionIconPicker";
import {
  COLLECTION_ICON_GROUPS,
  isCatalogIcon,
} from "../../../src/renderer/components/library/collection-icons";

/** v0.6 及以前硬编码在 SidebarLibraryPanel 里的那一组，库里已经有人在用 */
const LEGACY_PRESETS = [
  "📚",
  "💡",
  "🧠",
  "💻",
  "🎨",
  "🎬",
  "🎵",
  "🧪",
  "💼",
  "🌱",
  "⭐",
];

const ALL_ICONS = COLLECTION_ICON_GROUPS.flatMap((group) => group.icons);

describe("知识库图标目录", () => {
  it("旧版那 11 个预设一个不少", () => {
    // 删掉哪个，正用着它的知识库在选择器里就一格都不亮：
    // 用户看到的是「图标没了」，而库里的值其实还在
    for (const preset of LEGACY_PRESETS) {
      expect(isCatalogIcon(preset)).toBe(true);
    }
  });

  it("同一个图标不出现在两个分组里", () => {
    // 重复的那个被选中时会有两格同时高亮，看着像点错了
    expect(new Set(ALL_ICONS).size).toBe(ALL_ICONS.length);
  });
});

describe("知识库图标选择器", () => {
  beforeAll(async () => {
    await i18nReady;
    await changeLanguage("zh");
  });

  it("分组标题都列出来，图标按组摆开", () => {
    render(<CollectionIconPicker value="" onChange={vi.fn()} />);

    expect(screen.getByText("学习与研究")).toBeInTheDocument();
    expect(screen.getByText("标记与收纳")).toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(ALL_ICONS.length + 1);
  });

  it("没设图标时默认那格是选中态", () => {
    render(<CollectionIconPicker value="" onChange={vi.fn()} />);

    expect(screen.getByRole("button", { name: "默认图标" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("目录里的图标高亮在自己那一格上", () => {
    render(<CollectionIconPicker value="🎬" onChange={vi.fn()} />);

    expect(screen.getByRole("button", { name: "🎬" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "默认图标" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("目录外的图标单独摆一格并显示为已选", () => {
    // 旧 .NET 库迁移过来的 icon 与这份目录没有交集。不摆出来的话界面上
    // 一格都不亮，看着和「用的是默认图标」一模一样
    render(<CollectionIconPicker value="🦖" onChange={vi.fn()} />);

    expect(screen.getByRole("button", { name: /当前图标/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "默认图标" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("选了别的图标之后，目录外那格还在原地，能选回来", async () => {
    // 这一格跟着当前值走的话，点一下别的图标它就消失，原图标再也找不回来
    const user = userEvent.setup();
    const onChange = vi.fn();
    const { rerender } = render(
      <CollectionIconPicker value="🦖" onChange={onChange} />,
    );

    rerender(<CollectionIconPicker value="🎬" onChange={onChange} />);
    const custom = screen.getByRole("button", { name: /当前图标/ });
    expect(custom).toHaveAttribute("aria-pressed", "false");

    await user.click(custom);
    expect(onChange).toHaveBeenCalledWith("🦖");
  });
});
