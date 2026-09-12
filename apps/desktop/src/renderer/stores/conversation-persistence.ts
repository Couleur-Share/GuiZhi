/** 所有会话按 ID 串行写入，旧快照不能在新快照后落库。 */
const queues = new Map<string, Promise<unknown>>();
export function queueConversationSave<T>(id: string, write: () => Promise<T>): Promise<T> {
  const job = (queues.get(id) ?? Promise.resolve()).catch(() => undefined).then(write);
  queues.set(id, job);
  void job.finally(() => { if (queues.get(id) === job) queues.delete(id); }).catch(() => undefined);
  return job;
}
