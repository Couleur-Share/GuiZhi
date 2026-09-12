import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { AskMessageCard } from '../../../src/renderer/components/ask/AskMessageCard';
import { ArticleMessageCard } from '../../../src/renderer/components/ask/ArticleMessageCard';
vi.mock('../../../src/renderer/components/ui/Toast', () => ({ useToast: () => ({ showToast: vi.fn() }) }));

it.each(['knowledge', 'article'])('%s 证据清除后，已经展开的片段与当前版本链接同步消失', kind => {
  const source: any = { ordinal: 1, kind: kind === 'knowledge' ? 'item' : 'article', refId: 'source', target: { itemId: 'source', view: 'body' }, title: '可点的来源', evidence: { version: 1, kind: 'item', sourceId: 'source', title: '历史来源', text: '应被清除的历史片段', fingerprint: 'old', capturedAt: 1, reviewStatus: 'clear', reviewReasons: [] } };
  const message: any = { id: 'm', question: '问题', answer: '保留的答案', status: 'done', sources: [source], steps: [], warnings: [], webStatus: 'off' };
  const card = (value: any) => kind === 'knowledge' ? <AskMessageCard message={value} /> : <ArticleMessageCard message={value} running={false} onRetry={() => {}} />;
  const view = render(card(message)); fireEvent.click(screen.getByRole('button', { name: /可点的来源/ }));
  expect(screen.getByText('应被清除的历史片段')).toBeInTheDocument();
  view.rerender(card({ ...message, sources: [{ ...source, title: '来源已清除', cleared: true, evidence: { ...source.evidence, title: '来源已清除', cleared: true, text: '' } }] }));
  expect(screen.queryByText('应被清除的历史片段')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: '查看当前版本' })).not.toBeInTheDocument();
  expect(screen.getByText('保留的答案')).toBeInTheDocument();
});
