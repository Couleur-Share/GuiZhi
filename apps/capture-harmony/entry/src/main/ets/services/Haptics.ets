import { vibrator } from '@kit.SensorServiceKit';
import { AppPreferences } from './AppPreferences';

// 发送成功的一次轻触觉（系统预置的“成功通知”效果，随设备振动马达自行适配强度）。
// 需要 ohos.permission.VIBRATE（normal / system_grant，安装即授予，无运行时弹窗）。
// 仅在“按压反馈”开启且未开启“减少动效”时触发，与其它交互反馈遵循同一组偏好。
// 触觉只是辅助反馈：设备没有该效果、权限未生效、系统关闭了触感等情况一律静默忽略，
// 绝不能影响发送结果本身的呈现（提示与列表更新不依赖它）。
export function hapticSuccess(settings: AppPreferences): void {
  if (!settings.lightFeedback || settings.reduceMotion) return;
  try {
    const effectId: string = vibrator.HapticFeedback.EFFECT_NOTICE_SUCCESS;
    if (!vibrator.isSupportEffectSync(effectId)) return;
    const effect: vibrator.VibratePreset = { type: 'preset', effectId: effectId, count: 1 };
    const attribute: vibrator.VibrateAttribute = { usage: 'touch' };
    vibrator.startVibration(effect, attribute).catch((): void => {});
  } catch {
    // 同步异常（201 无权限、401 参数、801 不支持、14600101 设备故障）同样忽略。
  }
}
