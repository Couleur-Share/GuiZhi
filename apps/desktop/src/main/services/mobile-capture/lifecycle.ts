let stop: ((pause: boolean) => void) | undefined;
export function setMobileCaptureStop(handler: (pause: boolean) => void) { stop?.(false); stop = handler; }
/** 先同步停止定时器并取消请求，再关闭/替换数据库。 */
export function stopMobileCapture(pause = false) { stop?.(pause); }
