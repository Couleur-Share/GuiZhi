import { UIContext, curves } from '@kit.ArkUI';
import { AppPreferences } from './AppPreferences';

export function motionDuration(settings: AppPreferences): number {
  if (settings.reduceMotion) return 0;
  return settings.animationSpeed === 'fast' ? 180 : settings.animationSpeed === 'relaxed' ? 420 : 280;
}

export function animationSpeedLabel(value: string): string {
  return value === 'fast' ? '轻快' : value === 'relaxed' ? '舒缓' : '标准';
}

export function pressFeedback(settings: AppPreferences): ClickEffect {
  return { level: ClickEffectLevel.LIGHT, scale: settings.reduceMotion || !settings.lightFeedback ? 1 : 0.97 };
}

export function animateInterface(context: UIContext, settings: AppPreferences, change: () => void): void {
  if (settings.reduceMotion) { change(); return; }
  context.animateTo({ duration: motionDuration(settings), curve: curves.springMotion(0.35, 0.86) }, change);
}

// 列表轻微错峰，限制总延迟；减少动效时完全去掉缩放、位移和等待。
export function revealTransition(settings: AppPreferences, index: number = 0): TransitionEffect {
  if (settings.reduceMotion) return TransitionEffect.OPACITY.animation({ duration: 0 });
  return TransitionEffect.OPACITY.combine(TransitionEffect.translate({ y: 10 }))
    .combine(TransitionEffect.scale({ x: 0.98, y: 0.98 }))
    .animation({ duration: motionDuration(settings), curve: curves.springMotion(0.35, 0.86),
      delay: Math.min(Math.max(index, 0), 4) * 24 });
}
