import { common, ConfigurationConstant } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { THEME_COLOR_IDS } from './ThemePalette';
import { CaptureMode } from '../core/Protocol';

export class AppPreferences {
  theme: string = 'system';
  themeColor: string = 'blue';
  material: string = 'system';
  tabStyle: string = 'floating';
  lightFeedback: boolean = true;
  handedness: boolean = true;
  reduceMotion: boolean = false;
  animationSpeed: string = 'normal';
  defaultMode: CaptureMode = 'auto';
  previewLines: number = 3;
  showCount: boolean = true;
}

export class AppPreferencesStore {
  private prefs: preferences.Preferences;
  constructor(prefs: preferences.Preferences) { this.prefs = prefs; }
  static async open(context: common.Context): Promise<AppPreferencesStore> {
    return new AppPreferencesStore(await preferences.getPreferences(context, { name: 'guizhi_capture_settings_v1' }));
  }
  async load(): Promise<AppPreferences> {
    const raw = await this.prefs.get('appearance', '') as string;
    if (!raw) return new AppPreferences();
    const value = JSON.parse(raw) as AppPreferences;
    // 0.2.0 没有减少动效字段；补默认值但不改写用户的原有材质与主题。
    // 旧版固定绿色不视为用户选择；升级默认清爽蓝，保留其他偏好。
    if (value.themeColor === undefined) value.themeColor = 'blue';
    if (value.tabStyle === undefined) value.tabStyle = 'floating';
    if (value.animationSpeed === undefined) value.animationSpeed = 'normal';
    if (value.reduceMotion === undefined) value.reduceMotion = false;
    if (!['floating', 'standard'].includes(value.tabStyle) || !['system', 'light', 'dark'].includes(value.theme) || !THEME_COLOR_IDS.includes(value.themeColor) ||
      !['system', 'off', 'ultraThin', 'thin', 'regular', 'thick', 'ultraThick'].includes(value.material) ||
      !['auto', 'urls', 'text'].includes(value.defaultMode) || !['fast', 'normal', 'relaxed'].includes(value.animationSpeed) ||
      ![3, 6].includes(value.previewLines) || typeof value.lightFeedback !== 'boolean' ||
      typeof value.reduceMotion !== 'boolean' || typeof value.handedness !== 'boolean' || typeof value.showCount !== 'boolean') {
      throw new Error('设置读取失败，原设置未覆盖。请重试或联系开发者。');
    }
    return value;
  }
  async save(value: AppPreferences): Promise<void> {
    await this.prefs.put('appearance', JSON.stringify(value));
    await this.prefs.flush();
  }
}

export function applyColorMode(context: common.Context, value: AppPreferences): void {
  context.getApplicationContext().setColorMode(value.theme === 'light' ? ConfigurationConstant.ColorMode.COLOR_MODE_LIGHT :
    value.theme === 'dark' ? ConfigurationConstant.ColorMode.COLOR_MODE_DARK : ConfigurationConstant.ColorMode.COLOR_MODE_NOT_SET);
}
