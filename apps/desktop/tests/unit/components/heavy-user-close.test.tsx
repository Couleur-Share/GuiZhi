import { beforeEach, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, cleanup } from '@testing-library/react';
import { SaveBeforeClose } from '../../../src/renderer/components/app/SaveBeforeClose';
import { useKnowledgeStore } from '../../../src/renderer/stores/knowledge.store';
import { useAskStore } from '../../../src/renderer/stores/ask.store';
import { useArticleAskStore } from '../../../src/renderer/stores/article-ask.store';

beforeEach(() => {
  cleanup();
  useKnowledgeStore.setState({ hasUnsavedChanges: true, isSaving: false, saveError: '磁盘已满', flushPendingSave: vi.fn(async () => false) });
  useAskStore.setState({ messages: [], stop: vi.fn(), persist: vi.fn(async () => true) });
  useArticleAskStore.setState({ messages: [], stop: vi.fn(), persist: vi.fn(async () => true) });
});
it('正常退出保存失败不关闭；取消仍留在应用，成功重试才关闭', async () => {
  const close = vi.spyOn(window, 'close').mockImplementation(() => {});
  try {
    render(<SaveBeforeClose />);
    await act(async () => { window.dispatchEvent(new Event('beforeunload', { cancelable: true })); });
    expect(close).not.toHaveBeenCalled(); expect(screen.getByRole('alert').textContent).toContain('磁盘已满');
    fireEvent.click(screen.getByRole('button', { name: '取消退出' })); expect(close).not.toHaveBeenCalled();
    await act(async () => { window.dispatchEvent(new Event('beforeunload', { cancelable: true })); });
    useKnowledgeStore.setState({ flushPendingSave: vi.fn(async () => true) });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '重试保存并退出' })); });
    expect(close).toHaveBeenCalledOnce();
  } finally { close.mockRestore(); cleanup(); }
});
