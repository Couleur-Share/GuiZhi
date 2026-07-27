import { useMemo, useState } from "react";
import { ImageIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import {
  listAnchorBlocks,
  listIllustrations,
} from "@guizhi/shared/utils/illustration-note";
import { ACTION_CHIP } from "./detail-chips";
import { IllustrationPanel } from "./IllustrationPanel";

/**
 * 正文配图入口。
 *
 * 只在正文里确实存在「值得配图的段落」时出现——判定复用生成链路那套
 * listAnchorBlocks，而不是另拍一个字数阈值，两边不会各说各话。
 *
 * 图片条目不提供：它的详情页把正文拆成文案/图片/图中文字三个标签，
 * 而配图会被 splitImageNoteSections 从「文案」里摘走、混进「图片」标签页
 * 与原作配图排在一起，分不清哪张是原图、哪张是 AI 画的。
 */
export function IllustrationCard({ item }: { item: KnowledgeItem }) {
  const { t } = useTranslation();
  const [isOpen, setIsOpen] = useState(false);
  // 编辑正文时 ItemDetail 每次击键都重渲染，长文的整篇切块不能白跑
  const { hasAnchors, count } = useMemo(
    () => ({
      hasAnchors: listAnchorBlocks(item.content).length > 0,
      count: listIllustrations(item.content).length,
    }),
    [item.content],
  );

  if (item.itemType === "image" || !hasAnchors) {
    return null;
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setIsOpen(true)}
        className={ACTION_CHIP}
      >
        <ImageIcon className="h-3.5 w-3.5" aria-hidden="true" />
        {count > 0
          ? t("library.illustrationManage", "正文配图（{{count}} 张）", {
              count,
            })
          : t("library.illustrationCreate", "生成正文配图")}
      </button>
      {isOpen ? (
        <IllustrationPanel
          item={item}
          isOpen={isOpen}
          onClose={() => setIsOpen(false)}
        />
      ) : null}
    </>
  );
}
