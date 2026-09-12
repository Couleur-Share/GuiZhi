import { useCallback, useEffect, useRef, useState } from "react";
import type { ReadingPanelTab } from "../library/reading-memory";
import type { ThemedReadingSourceKind } from "@guizhi/shared/types/themed-reading";

/** 内容版本与编辑状态分开；每次打开有成品时默认阅读 AI 页。 */
export function useThemedReadingMode(
  itemId: string,
  tab: ReadingPanelTab,
  enabled: boolean,
) {
  const sourceKind: ThemedReadingSourceKind =
    tab === "summary" ? "summary" : "body";
  const available = enabled && (tab === "body" || tab === "summary");
  const key = `${itemId}:${sourceKind}`;
  const [state, setState] = useState({ key, active: false, editing: false });
  const selection = useRef(0);
  const editing = state.key === key && state.editing;
  const editingRef = useRef(editing);
  editingRef.current = editing;
  useEffect(() => {
    let live = true;
    const initialSelection = selection.current;
    setState({ key, active: false, editing: false });
    if (available)
      window.api.themedReading
        .getState?.({ itemId, sourceKind })
        .then((r) => {
          if (
            live &&
            selection.current === initialSelection &&
            r.success &&
            r.state &&
            !editingRef.current
          )
            setState({ key, active: r.state.hasPage, editing: false });
        })
        .catch(() => {
          /* 完整阅读面板负责展示可重试读取错误。 */
        });
    return () => {
      live = false;
    };
  }, [itemId, sourceKind, key, available]);
  // 完成事件由阅读面板刷新成品；它不改变这里的视图选择，避免后台任务抢占原文或编辑。
  const select = useCallback(
    (active: boolean) => {
      selection.current++;
      setState({ key, active, editing: false });
    },
    [key],
  );
  return {
    sourceKind,
    available,
    active: available && state.key === key && state.active && !editing,
    editing,
    select,
    toggle: () => select(!state.active),
    startEditing: () => {
      selection.current++;
      setState({ key, active: false, editing: true });
    },
    finishEditing: () => select(false),
  };
}
