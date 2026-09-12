import { afterEach, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ workers: [] as any[] }));
vi.mock('node:worker_threads', async () => { const { EventEmitter } = await import('node:events'); class FakeWorker extends EventEmitter {
  terminate = vi.fn(async () => 0); unref() {}
  constructor() { super(); mocks.workers.push(this); }
  postMessage(message: any) { if (message.action === 'init') queueMicrotask(() => this.emit('message', { id: message.id, ready: true })); else (this as any).query = message; }
} return { Worker: FakeWorker, default: { Worker: FakeWorker } }; });
import { getSemanticWorker, resetSemanticWorkers, workerGenerationCount } from '../../../src/main/services/semantic-worker-client';
afterEach(() => { resetSemanticWorkers(); mocks.workers.length = 0; });
it('十次代际更新最多保留两代；旧查询完成后才释放其工作线程', async () => {
  const input = { cache: { model: 'm', dims: 2, vectors: new Float32Array([1,0]), itemIds: ['a'], chunkIndexes: [0] }, model: 'm', rootDir: 'unused-fixture-root' };
  const first = await getSemanticWorker({ ...input, generation: '0' });
  const pending = first.search(new Float32Array([1,0]), 1), worker = mocks.workers[0];
  for (let n = 1; n < 10; n++) { await getSemanticWorker({ ...input, generation: String(n) }); expect(workerGenerationCount()).toBeLessThanOrEqual(2); }
  expect(worker.terminate).not.toHaveBeenCalled();
  worker.emit('message', { id: worker.query.id, hits: [{ itemId: 'a', chunkIndex: 0, score: 1 }] });
  expect(await pending).toHaveLength(1); expect(worker.terminate).toHaveBeenCalledOnce();
});
