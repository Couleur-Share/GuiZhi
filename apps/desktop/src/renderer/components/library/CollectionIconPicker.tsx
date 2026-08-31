import { useLayoutEffect, useRef } from "react";
import { FolderIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { COLLECTION_ICON_GROUPS, isCatalogIcon } from "./collection-icons";

const SWATCH_BASE =
  "flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border text-base leading-none transition-colors";
const PILL_BASE =
  "inline-flex h-9 shrink-0 items-center gap-1.5 rounded-lg border px-2.5 text-xs transition-colors";
const SELECTED = "border-primary/50 bg-primary/10 text-primary";
const IDLE = "border-border text-muted-foreground hover:bg-muted/60";

/**
 * 知识库图标选择器：默认图标一行钉在上面，其余按 `collection-icons.ts`
 * 的分组滚动列出。
 *
 * 分组标题不做成筛选标签页，是因为「🎓 该算学习还是生活」这种归类
 * 只有作者自己清楚，逼用户先猜对分类才看得见图标，比一片平铺还慢；
 * 一条滚动带小标题则是纯粹的扫视辅助，猜错了往下看就行。
 *
 * 默认图标不跟着滚：它同时是「清掉图标」的唯一入口，滑到第八组还得
 * 翻回顶部才点得到。
 */
export function CollectionIconPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (icon: string) => void;
}) {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);
  const selectedRef = useRef<HTMLButtonElement>(null);

  // 目录外的图标（多半来自旧 .NET 库迁移）单独摆一格，否则重命名时
  // 界面上一格都不亮，用户会以为图标已经丢了。
  // 记的是打开时那一个而不是当前值：跟着 value 走的话，点一下别的图标
  // 这一格就消失，原来那个图标再也选不回来。
  const customIcon = useRef(
    value && !isCatalogIcon(value) ? value : "",
  ).current;

  // 重命名时把已选的那格滚进视野。八组共八行，选中的多半在屏幕外，
  // 不滚的话看到的是「什么都没选」——与上面那格的理由相同。
  // 用容器坐标手算而不是 scrollIntoView：后者会连带滚动弹窗本身的
  // 滚动容器，开着弹窗跳一下。
  useLayoutEffect(() => {
    const button = selectedRef.current;
    const container = scrollRef.current;
    if (!button || !container) {
      return;
    }
    container.scrollTop = Math.max(
      0,
      button.offsetTop - (container.clientHeight - button.offsetHeight) / 2,
    );
  }, []);

  const swatchClass = (selected: boolean) =>
    `${SWATCH_BASE} ${selected ? SELECTED : IDLE}`;
  const pillClass = (selected: boolean) =>
    `${PILL_BASE} ${selected ? SELECTED : IDLE}`;

  return (
    <div className="space-y-1.5">
      <span className="text-xs text-muted-foreground">
        {t("library.collectionIcon", "图标")}
      </span>

      {/* 这两个不是「另外两个图标」，写上文字才说得清各自是什么 */}
      <div className="flex items-center gap-1.5 px-2">
        <button
          type="button"
          onClick={() => onChange("")}
          aria-pressed={value === ""}
          className={pillClass(value === "")}
        >
          <FolderIcon className="h-4 w-4" aria-hidden="true" />
          {t("library.collectionIconDefault", "默认图标")}
        </button>
        {customIcon ? (
          <button
            type="button"
            onClick={() => onChange(customIcon)}
            aria-pressed={value === customIcon}
            className={pillClass(value === customIcon)}
          >
            <span className="text-base leading-none">{customIcon}</span>
            {t("library.collectionIconCurrent", "当前图标")}
          </button>
        ) : null}
      </div>

      <div
        ref={scrollRef}
        className="relative max-h-64 overflow-y-auto rounded-xl border border-border/60 bg-muted/20 p-2"
      >
        {COLLECTION_ICON_GROUPS.map((group) => (
          <div key={group.id} className="mb-2 last:mb-0">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {t(group.labelKey, group.fallback)}
            </span>
            <div className="mt-1 flex flex-wrap gap-1">
              {group.icons.map((icon) => (
                <button
                  key={icon}
                  ref={value === icon ? selectedRef : undefined}
                  type="button"
                  onClick={() => onChange(icon)}
                  aria-pressed={value === icon}
                  className={swatchClass(value === icon)}
                >
                  {icon}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
