import { useEffect, useRef, useState } from 'react';
import { useKnowledgeStore } from '../../stores/knowledge.store';
import { useAskStore } from "../../stores/ask.store";
import { useArticleAskStore } from "../../stores/article-ask.store";
import { Button } from "../ui/Button";
import { Modal } from '../ui/Modal';

/** 正常关闭必须获得成功回执；保存失败只由用户明确放弃才能关闭。 */
export function SaveBeforeClose() {
  const allowed = useRef(false), flushing = useRef(false);
  const [error, setError] = useState<string | null>(null);
  const attempt = async () => {
    if (flushing.current) return;
    flushing.current = true;
    try {
      useAskStore.getState().stop(); useArticleAskStore.getState().stop();
      const draft = await useKnowledgeStore.getState().flushPendingSave();
      const ask = await useAskStore.getState().persist();
      const article = await useArticleAskStore.getState().persist();
      if (draft && ask && article) { allowed.current = true; window.close(); }
      else setError(useKnowledgeStore.getState().saveError || useAskStore.getState().saveError || useArticleAskStore.getState().saveError || '内容尚未保存');
    } catch (e) { setError(e instanceof Error ? e.message : String(e));
    } finally { flushing.current = false; }
  };
  useEffect(() => {
    const close = (event: BeforeUnloadEvent) => {
      if (allowed.current || (!useKnowledgeStore.getState().hasUnsavedChanges && !useKnowledgeStore.getState().isSaving && !useAskStore.getState().messages.length && !useArticleAskStore.getState().messages.length)) return;
      event.preventDefault(); event.returnValue = ''; void attempt();
    };
    window.addEventListener('beforeunload', close);
    return () => window.removeEventListener('beforeunload', close);
  }, []);
  return <Modal isOpen={error !== null} onClose={() => setError(null)} title="尚有未保存的内容">
    <p className="whitespace-pre-wrap text-sm" role="alert">{error}</p>
    <div className="mt-4 flex flex-wrap gap-4">
      <Button variant="secondary" onClick={() => void attempt()}>重试保存并退出</Button>
      <Button variant="secondary" onClick={() => setError(null)}>取消退出</Button>
      <Button variant="danger" onClick={() => { allowed.current = true; window.close(); }}>放弃未保存内容并退出</Button>
    </div>
  </Modal>;
}
