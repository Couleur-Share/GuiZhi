export interface CollectionIconGroup {
  id: string;
  labelKey: string;
  fallback: string;
  icons: string[];
}

/**
 * 知识库图标目录。
 *
 * 分八组、每组十个，一组正好占一行——扫一眼就是一整类，比一片平铺的
 * emoji 好找得多。分组名按「用户会拿知识库装什么」来切（学习、工作、
 * 技术、创作……），不按 Unicode 自己的表情/物件/符号分类：后者是给输入法
 * 用的，在这里等于没分。
 *
 * 只收广为流传的老 emoji（Unicode 11 及以前为主）。新码位在 Windows 10 的
 * Segoe UI Emoji 上会渲染成豆腐块，而这一格的全部用处就是让人一眼认出来。
 *
 * 三条改动约束：
 * - 全目录不得有重复。同一个 emoji 出现两次，选中时会有两格同时高亮。
 * - v0.6 及以前那 11 个预设（📚 💡 🧠 💻 🎨 🎬 🎵 🧪 💼 🌱 ⭐）必须留在表里。
 *   删掉哪个，已经用着它的知识库在选择器里就一格都不亮，用户以为图标丢了。
 * - 每组十个是排版前提，不是硬性校验；真要加第十一个会折行，不会出错。
 */
export const COLLECTION_ICON_GROUPS: CollectionIconGroup[] = [
  {
    id: "learning",
    labelKey: "library.iconGroupLearning",
    fallback: "学习与研究",
    icons: ["📚", "📖", "🎓", "🧠", "💡", "✏️", "📝", "🔬", "🧪", "🔭"],
  },
  {
    id: "work",
    labelKey: "library.iconGroupWork",
    fallback: "工作与项目",
    icons: ["💼", "📊", "📈", "🗓️", "✅", "📌", "🎯", "🏢", "🤝", "⏰"],
  },
  {
    id: "tech",
    labelKey: "library.iconGroupTech",
    fallback: "技术与开发",
    icons: ["💻", "🖥️", "⌨️", "🧩", "⚙️", "🔧", "🐛", "🤖", "🔐", "🌐"],
  },
  {
    id: "creative",
    labelKey: "library.iconGroupCreative",
    fallback: "创作与设计",
    icons: ["🎨", "🖌️", "✍️", "🎬", "📷", "🎵", "🎤", "🖼️", "🎭", "✨"],
  },
  {
    id: "life",
    labelKey: "library.iconGroupLife",
    fallback: "生活与健康",
    icons: ["🏠", "☕", "🍳", "🌿", "🌱", "💪", "🏃", "🧘", "🛏️", "🐾"],
  },
  {
    id: "business",
    labelKey: "library.iconGroupBusiness",
    fallback: "财务与商务",
    icons: ["💰", "💵", "💳", "🏦", "🧾", "🛒", "🏷️", "📦", "💹", "🎁"],
  },
  {
    id: "travel",
    labelKey: "library.iconGroupTravel",
    fallback: "旅行与自然",
    icons: ["✈️", "🗺️", "🧭", "🏔️", "🌊", "🌍", "🏕️", "🚗", "🌸", "☀️"],
  },
  {
    id: "marks",
    labelKey: "library.iconGroupMarks",
    fallback: "标记与收纳",
    icons: ["⭐", "❤️", "🔥", "⚡", "🔖", "🏆", "🗂️", "🗃️", "🔗", "❗"],
  },
];

const CATALOG_ICONS = new Set(
  COLLECTION_ICON_GROUPS.flatMap((group) => group.icons),
);

/**
 * 图标在不在目录里。
 *
 * 目录外的取值真会出现：旧 .NET 库的 `Collections.Icon` 是照搬进来的，
 * 那套图标与这里没有交集。选择器据此把它单独摆一格并显示为已选，
 * 否则界面上一格都不亮，看着就像默认图标——而用户一旦点了别处，
 * 原来那个图标就再也回不来了。
 */
export function isCatalogIcon(icon: string): boolean {
  return CATALOG_ICONS.has(icon);
}
