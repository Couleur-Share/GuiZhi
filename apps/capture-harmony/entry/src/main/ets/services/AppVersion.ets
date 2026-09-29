import { bundleManager } from '@kit.AbilityKit';

// 关于页的版本文案：读取安装包自身的 versionName，不再手写版本号，避免升级后忘记同步。
// 开发包（包名以 .dev 结尾）追加标记，避免与正式包混淆。
export function appVersionLabel(): string {
  try {
    const info = bundleManager.getBundleInfoForSelfSync(bundleManager.BundleFlag.GET_BUNDLE_INFO_DEFAULT);
    if (!info.versionName) return '版本未知';
    return info.name.endsWith('.dev') ? `${info.versionName} · 开发包` : info.versionName;
  } catch {
    return '版本未知';
  }
}
