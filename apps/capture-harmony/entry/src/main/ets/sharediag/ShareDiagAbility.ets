// 分享入口 spike（仅 dev 包，由 hvigorfile.ts 注册）：桌面上单独的“分享诊断”入口，用来查看并复制诊断报告。
// 它与归知收集共用同一个应用进程，因此打开时记录的 pid 即主进程基线，用来判断分享扩展是否独立进程。
import { UIAbility } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { hilog } from '@kit.PerformanceAnalysisKit';

export default class ShareDiagAbility extends UIAbility {
  onWindowStageCreate(windowStage: window.WindowStage): void {
    windowStage.loadContent('sharediag/ShareDiagReport', (error) => {
      if (error.code) hilog.error(0x0000, 'guizhi', '分享诊断页加载失败，错误码 %{public}d', error.code);
    });
  }
}
