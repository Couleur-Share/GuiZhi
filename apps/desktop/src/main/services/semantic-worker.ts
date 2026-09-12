import { parentPort } from 'node:worker_threads';
import { HnswSemanticSearchBackend, ExactSemanticSearchBackend, type SemanticSearchBackend } from './semantic-search-backend';

let backend: SemanticSearchBackend | null = null;
parentPort?.on('message', async (message) => {
  try {
    if (message.action === 'init') {
      try { backend = await HnswSemanticSearchBackend.create(message.input); }
      catch (error) {
        // 运行时不兼容与单份侧车损坏区分；侧车损坏由构建器重建。
        parentPort?.postMessage({ id: message.id, error: String(error), incompatible: String(error).includes('HNSW_RUNTIME:') }); return;
      }
      parentPort?.postMessage({ id: message.id, ready: true });
    } else if (message.action === 'exact') {
      backend = new ExactSemanticSearchBackend(message.cache);
      parentPort?.postMessage({ id: message.id, ready: true });
    } else if (message.action === 'search') {
      if (!backend) throw new Error('索引尚未就绪');
      parentPort?.postMessage({ id: message.id, hits: await backend.search(message.query, message.limit) });
    }
  } catch (error) { parentPort?.postMessage({ id: message.id, error: String(error) }); }
});
