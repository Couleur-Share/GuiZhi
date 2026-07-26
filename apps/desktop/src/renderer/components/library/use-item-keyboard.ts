/**
 * 条目列表的键盘导航（卡片视图与列表视图共用）。
 *
 * 之前整个 library 目录里只有三处 onKeyDown（两个输入框加一个拖拽手柄），
 * 收件箱里几十条待整理只能一路点鼠标。
 */
import { useEffect } from "react";
import { useKnowledgeStore } from "../../stores/knowledge.store";

/** 焦点在可编辑控件里时，方向键属于那个控件 */
function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tag = target.tagName;
  return (
    tag === "INPUT" ||
    tag === "TEXTAREA" ||
    tag === "SELECT" ||
    target.isContentEditable ||
    // CodeMirror 的编辑区是 contenteditable，但内部还有一层包装
    target.closest(".cm-editor") !== null
  );
}

/** 按钮/链接自己会处理 Enter 与 Space，不抢它们的 */
function isActivatableTarget(target: EventTarget | null): boolean {
  return (
    target instanceof HTMLElement &&
    target.closest("button, a, [role='menuitem']") !== null
  );
}

export interface ItemKeyboardOptions {
  /** 详情浮层打开等场景下整体停用 */
  enabled?: boolean;
  /** Enter 打开条目；卡片视图右侧常驻详情栏，可不传 */
  onOpen?: (id: string) => void;
  /** Delete 删除；回收站里语义是彻底删除，由调用方决定要不要弹确认 */
  onDelete?: (ids: string[]) => void;
}

export function useItemListKeyboard({
  enabled = true,
  onOpen,
  onDelete,
}: ItemKeyboardOptions = {}): void {
  useEffect(() => {
    if (!enabled) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.ctrlKey || event.metaKey || event.altKey) {
        return;
      }
      if (isEditableTarget(event.target)) {
        return;
      }

      const state = useKnowledgeStore.getState();
      const { entries, selectedId, selectionIds } = state;
      if (entries.length === 0) {
        return;
      }
      const currentIndex = entries.findIndex((entry) => entry.id === selectedId);

      const moveTo = (index: number) => {
        const clamped = Math.max(0, Math.min(index, entries.length - 1));
        const target = entries[clamped];
        if (target && target.id !== selectedId) {
          void state.selectItem(target.id);
        }
      };

      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          moveTo(currentIndex < 0 ? 0 : currentIndex + 1);
          return;
        case "ArrowUp":
          event.preventDefault();
          moveTo(currentIndex < 0 ? entries.length - 1 : currentIndex - 1);
          return;
        case "Home":
          event.preventDefault();
          moveTo(0);
          return;
        case "End":
          event.preventDefault();
          moveTo(entries.length - 1);
          return;
        case "Enter": {
          if (!onOpen || !selectedId || isActivatableTarget(event.target)) {
            return;
          }
          event.preventDefault();
          onOpen(selectedId);
          return;
        }
        case " ": {
          if (!selectedId || isActivatableTarget(event.target)) {
            return;
          }
          event.preventDefault();
          state.toggleSelection(selectedId);
          return;
        }
        case "Delete": {
          if (!onDelete) {
            return;
          }
          // 有多选时删多选，否则删当前条目
          const ids = selectionIds.length > 0 ? selectionIds : selectedId ? [selectedId] : [];
          if (ids.length === 0) {
            return;
          }
          event.preventDefault();
          onDelete(ids);
          return;
        }
        default:
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [enabled, onOpen, onDelete]);
}
