// 仅 dev 产品由 Hvigor 注册，不读取主应用连接或知识库。
import { UIAbility } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { hilog } from '@kit.PerformanceAnalysisKit';

export default class ReadingDiagAbility extends UIAbility {
  onWindowStageCreate(stage: window.WindowStage): void {
    stage.loadContent('syncdiag/ReadingDiagPage', (error) => {
      if (error.code) hilog.error(0x0000, 'guizhi', '阅读同步诊断加载失败，错误码 %{public}d', error.code);
    });
  }
}
