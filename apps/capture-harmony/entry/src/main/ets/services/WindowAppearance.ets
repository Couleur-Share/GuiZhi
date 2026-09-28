import { common } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';

// 背景铺满窗口，内容使用真实避让区；不隐藏状态栏和系统手势条。
export class WindowAppearance {
  private mainWindow: window.Window | undefined;
  private context: common.UIAbilityContext | undefined;
  private listening: boolean = false;
  private onAvoidArea = (_event: window.AvoidAreaOptions): void => {
    try { this.updateInsets(); }
    catch {
      this.restoreSafeLayout().catch(() => {
        AppStorage.setOrCreate('captureError', '屏幕安全区域更新失败，请重新打开应用重试。');
      });
    }
  };

  async attach(stage: window.WindowStage, context: common.UIAbilityContext): Promise<void> {
    this.context = context;
    const main = stage.getMainWindowSync();
    this.mainWindow = main;
    try {
      await main.setWindowLayoutFullScreen(true);
      this.updateInsets();
      await this.updateColors();
      main.on('avoidAreaChange', this.onAvoidArea);
      this.listening = true;
    } catch {
      await this.restoreSafeLayout();
    }
  }

  private updateInsets(): void {
    const main = this.mainWindow;
    if (!main) return;
    const system = main.getWindowAvoidArea(window.AvoidAreaType.TYPE_SYSTEM);
    const cutout = main.getWindowAvoidArea(window.AvoidAreaType.TYPE_CUTOUT);
    const navigation = main.getWindowAvoidArea(window.AvoidAreaType.TYPE_NAVIGATION_INDICATOR);
    const keyboard = main.getWindowAvoidArea(window.AvoidAreaType.TYPE_KEYBOARD);
    AppStorage.setOrCreate('captureSafeTop', Math.max(system.topRect.height, cutout.topRect.height));
    AppStorage.setOrCreate('captureSafeBottom', Math.max(system.bottomRect.height, navigation.bottomRect.height));
    AppStorage.setOrCreate('captureSafeLeft', Math.max(system.leftRect.width, cutout.leftRect.width));
    AppStorage.setOrCreate('captureSafeRight', Math.max(system.rightRect.width, cutout.rightRect.width));
    AppStorage.setOrCreate('captureKeyboardVisible', keyboard.bottomRect.height > 0);
  }

  async updateColors(): Promise<void> {
    if (!this.mainWindow || !this.context) return;
    const resources = this.context.resourceManager;
    const foreground = '#' + resources.getColorByNameSync('text_primary').toString(16).padStart(8, '0');
    await this.mainWindow.setWindowSystemBarProperties({
      statusBarColor: '#00000000', navigationBarColor: '#00000000',
      statusBarContentColor: foreground, navigationBarContentColor: foreground
    });
  }

  private async restoreSafeLayout(): Promise<void> {
    const main = this.mainWindow;
    if (!main) return;
    // 回退后注销监听，避免继续写入手工边距造成二次避让。
    if (this.listening) { main.off('avoidAreaChange', this.onAvoidArea); this.listening = false; }
    await main.setWindowLayoutFullScreen(false);
    this.clearInsets();
    AppStorage.setOrCreate('captureError', '系统栏适配未完成，已使用系统安全布局，请重新打开应用重试。');
  }

  private clearInsets(): void {
    AppStorage.setOrCreate('captureSafeTop', 0); AppStorage.setOrCreate('captureSafeBottom', 0);
    AppStorage.setOrCreate('captureSafeLeft', 0); AppStorage.setOrCreate('captureSafeRight', 0);
    AppStorage.setOrCreate('captureKeyboardVisible', false);
  }

  detach(): void {
    if (this.listening) this.mainWindow?.off('avoidAreaChange', this.onAvoidArea);
    this.listening = false; this.mainWindow = undefined; this.context = undefined;
  }
}
