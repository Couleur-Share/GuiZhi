let stop: ((pause: boolean) => void) | undefined;
export function setReadingSyncStop(next: (pause: boolean) => void) { stop?.(false); stop = next; }
export function stopReadingSync(pause = false) { stop?.(pause); }
