export interface BufferedSubscription<T> {
  publish: (value: T) => void;
  subscribe: (listener: (value: T) => void) => () => void;
}

export function createBufferedSubscription<T>(
  maxPending = 10,
): BufferedSubscription<T> {
  if (!Number.isInteger(maxPending) || maxPending <= 0) {
    throw new Error("maxPending must be a positive integer");
  }

  const listeners = new Set<(value: T) => void>();
  const pending: T[] = [];

  return {
    publish(value) {
      if (listeners.size === 0) {
        pending.push(value);
        if (pending.length > maxPending) {
          pending.shift();
        }
        return;
      }

      for (const listener of listeners) {
        listener(value);
      }
    },
    subscribe(listener) {
      listeners.add(listener);
      const buffered = pending.splice(0, pending.length);
      for (const value of buffered) {
        listener(value);
      }
      return () => {
        listeners.delete(listener);
      };
    },
  };
}
