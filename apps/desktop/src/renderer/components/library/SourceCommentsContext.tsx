import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { KnowledgeItem, SourceComment } from "@guizhi/shared/types";
import { detectSourceCommentsPlatform } from "@guizhi/shared/utils/platform-capture";
import { useTranslation } from "react-i18next";
import { useToast } from "../ui/Toast";

function useComments(item: KnowledgeItem) {
  const supported =
    !item.deletedAt && !!detectSourceCommentsPlatform(item.sourceUri ?? "");
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [comments, setComments] = useState<SourceComment[]>([]);
  const [open, setOpen] = useState(false);
  const [limit, setLimit] = useState<10 | 20 | 50>(20);
  const [reading, setReading] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempted, setAttempted] = useState(false);
  const request = useRef(0);
  const capturing = useRef(false);

  // Provider 按条目重建；请求序号同时拦截卸载后的迟到结果与过期的本地读取。
  const load = useCallback(async () => {
    if (!supported) return;
    const current = ++request.current;
    setReading(true);
    setLoadError(null);
    try {
      const result = await window.api.platformCapture.listComments(item.id);
      if (current === request.current) setComments(result);
    } catch (cause) {
      if (current === request.current)
        setLoadError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (current === request.current) setReading(false);
    }
  }, [item.id, supported]);

  useEffect(() => {
    void load();
    return () => {
      request.current += 1;
    };
  }, [load]);

  const refresh = async () => {
    if (!supported || capturing.current || reading || loadError) return;
    capturing.current = true;
    const current = ++request.current;
    setLoading(true);
    setError(null);
    setAttempted(true);
    try {
      const result = await window.api.platformCapture.refreshComments({
        itemId: item.id,
        limit,
      });
      if (current === request.current) setComments(result);
    } catch (cause) {
      if (current !== request.current) return;
      const detail = cause instanceof Error ? cause.message : String(cause);
      setError(detail);
      showToast(t("library.commentsCaptureFailed", "采集评论失败"), "error", {
        detail,
      });
    } finally {
      capturing.current = false;
      if (current === request.current) setLoading(false);
    }
  };

  return {
    supported,
    comments,
    open,
    setOpen,
    limit,
    setLimit,
    reading,
    loading,
    loadError,
    error,
    attempted,
    load,
    refresh,
  };
}

const SourceCommentsContext = createContext<ReturnType<
  typeof useComments
> | null>(null);

export function SourceCommentsProvider({
  item,
  children,
}: {
  item: KnowledgeItem;
  children: ReactNode;
}) {
  const state = useComments(item);
  return (
    <SourceCommentsContext.Provider value={state}>
      {children}
    </SourceCommentsContext.Provider>
  );
}

export const useSourceComments = () => useContext(SourceCommentsContext);
