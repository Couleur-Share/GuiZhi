/**
 * 本地转写引擎（托管 funasr-server + SenseVoiceSmall）的固定标识与地址判定。
 *
 * 主进程按它写入 / 移除 ai-models.json 里的内置条目，渲染进程按它挡住
 * 「在模型服务里删掉内置引擎」——那两条记录只在安装时写入、卸载时移除，
 * 没有任何自愈路径，删掉的后果是运行时文件还在而配置没了，只能重装。
 * 两侧算出不同的结果就等于放行一次不可自愈的删除，所以只留这一份。
 */

/** 服务固定监听端口（仅绑定 127.0.0.1） */
export const FUNASR_PORT = 8620;
export const FUNASR_BASE_URL = `http://127.0.0.1:${FUNASR_PORT}/v1`;

/** ai-models.json 里内置条目的固定 id（安装时写入、卸载时移除） */
export const FUNASR_PROVIDER_ID = "provider_local_funasr";
export const FUNASR_MODEL_ID = "model_local_sensevoice";

/** 判断 apiUrl 是否指向托管的本地转写服务 */
export function isManagedFunasrUrl(apiUrl: string): boolean {
  try {
    const url = new URL(apiUrl.trim().replace(/#$/, ""));
    return (
      (url.hostname === "127.0.0.1" || url.hostname === "localhost") &&
      url.port === String(FUNASR_PORT)
    );
  } catch {
    return false;
  }
}

/** 固定 id 认安装时写入的那条，地址认用户手工添加的同一个服务 */
export function isLocalEngineProvider(provider: {
  id?: string;
  apiUrl: string;
}): boolean {
  return (
    provider.id === FUNASR_PROVIDER_ID || isManagedFunasrUrl(provider.apiUrl)
  );
}
