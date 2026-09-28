import { uiMaterial } from '@kit.ArkUI';
import { deviceInfo } from '@kit.BasicServicesKit';
import { AppPreferences } from './AppPreferences';

export function materialAvailable(): boolean {
  if (typeof deviceInfo.apiAvailable !== 'function') return false;
  if (deviceInfo.apiAvailable('26.0.0')) return uiMaterial.isImmersiveMaterialSupported();
  return false;
}

// 名称和五档枚举对应官方 ImmersiveStyle；沿用旧版本保存的 thin/regular/thick 值。
export function materialLabel(value: string): string {
  switch (value) {
    case 'ultraThin': return '超薄';
    case 'thin': return '薄';
    case 'regular': return '常规';
    case 'thick': return '厚';
    case 'ultraThick': return '超厚';
    case 'off': return '关闭';
    default: return '自动（按组件）';
  }
}

function materialStyle(value: string, panel: boolean): uiMaterial.ImmersiveStyle | undefined {
  if (typeof deviceInfo.apiAvailable !== 'function') return undefined;
  if (deviceInfo.apiAvailable('26.0.0')) {
    switch (value) {
      case 'ultraThin': return uiMaterial.ImmersiveStyle.ULTRA_THIN;
      case 'thin': return uiMaterial.ImmersiveStyle.THIN;
      case 'regular': return uiMaterial.ImmersiveStyle.REGULAR;
      case 'thick': return uiMaterial.ImmersiveStyle.THICK;
      case 'ultraThick': return uiMaterial.ImmersiveStyle.ULTRA_THICK;
      default: return panel ? uiMaterial.ImmersiveStyle.THICK : uiMaterial.ImmersiveStyle.THIN;
    }
  }
  return undefined;
}

export function panelMaterial(settings: AppPreferences): uiMaterial.Material | undefined {
  if (typeof deviceInfo.apiAvailable !== 'function') return undefined;
  if (deviceInfo.apiAvailable('26.0.0')) {
    if (settings.material === 'off') return uiMaterial.Material.empty;
    if (!uiMaterial.isImmersiveMaterialSupported()) return undefined;
    return new uiMaterial.ImmersiveMaterial({
      style: materialStyle(settings.material, true),
      interactive: settings.lightFeedback && !settings.reduceMotion, lightEffect: settings.lightFeedback ? { color: Color.White } : null
    });
  }
  return undefined;
}

export class CaptureTabsAppearance implements AttributeModifier<TabsAttribute> {
  private hidden: boolean;
  private settings: AppPreferences;
  constructor(hidden: boolean, settings: AppPreferences) { this.hidden = hidden; this.settings = settings; }
  applyNormalAttribute(tabs: TabsAttribute): void {
    // 旧系统不存在新方法；在属性调用和枚举解引用之前判断版本。
    if (typeof deviceInfo.apiAvailable !== 'function') {
      tabs.barBackgroundColor($r('app.color.page_background')); return;
    }
    if (deviceInfo.apiAvailable('26.0.0')) {
      if (this.hidden || this.settings.tabStyle === 'standard' || this.settings.material === 'off' || !uiMaterial.isImmersiveMaterialSupported()) {
        tabs.barFloatingStyle(undefined);
        tabs.barBackgroundColor($r('app.color.page_background'));
        return;
      }
      tabs.barBackgroundColor(Color.Transparent);
      tabs.barFloatingStyle({
        barSideMargin: 20, barBottomMargin: 12,
        maskColor: $r('app.color.page_background'), maskHeight: 96,
        adaptToHandedness: this.settings.handedness,
        systemMaterial: new uiMaterial.ImmersiveMaterial({
          style: materialStyle(this.settings.material, false),
          applyShadow: true, interactive: this.settings.lightFeedback && !this.settings.reduceMotion,
          lightEffect: this.settings.lightFeedback ? { color: Color.White } : null
        })
      });
    } else { tabs.barBackgroundColor($r('app.color.page_background')); }
  }
}
