import { hapTasks, OhosHapContext, OhosPluginId } from '@ohos/hvigor-ohos-plugin';
import { HvigorNode, HvigorPlugin } from '@ohos/hvigor';

// 分享入口 spike（结论见 docs/share-spike.md）：只给 dev 产品注册分享扩展与“分享诊断”入口，正式包的 module.json 不受影响。
// hvigor 不会把 src/dev/module.json5 合并进 main（已实测），只能在构建期按产品改写 module.json；
// 源码留在 src/main/ets/sharediag/：未被正式包引用的文件不会进入正式包（由 inspect-hap.ps1 -ForbidCode 核对）。
// 路由页面同理，由 src/dev/resources/base/profile/main_pages.json 只在 dev 包补充。
const SHARE_SPIKE_PRODUCT = 'dev';

// 分享面板匹配文本、图片与任意对象：spike 要看清各来源到底给了什么，所以放宽匹配；
// maxFileSupported 是扩展一次最多能处理的记录数，过小会让扩展不出现在分享面板里。
const shareDiagExtension = {
  name: 'ShareDiagExtAbility',
  type: 'share',
  exported: true,
  srcEntry: './ets/sharediag/ShareDiagExtAbility.ets',
  description: '$string:module_desc',
  icon: '$media:app_icon',
  label: '$string:launcher_name',
  skills: [{
    actions: ['ohos.want.action.sendData'],
    uris: [
      { scheme: 'file', utd: 'general.text', maxFileSupported: 9 },
      { scheme: 'file', utd: 'general.image', maxFileSupported: 9 },
      { scheme: 'file', utd: 'general.object', maxFileSupported: 9 },
    ],
  }],
};

const shareDiagAbility = {
  name: 'ShareDiagAbility',
  srcEntry: './ets/sharediag/ShareDiagAbility.ets',
  description: '$string:module_desc',
  icon: '$media:app_icon',
  label: '$string:diag_label',
  startWindowIcon: '$media:app_icon',
  startWindowBackground: '$color:page_background',
  exported: true,
  launchType: 'singleton',
  skills: [{ entities: ['entity.system.home'], actions: ['action.system.home'] }],
};

// 阅读密码互操作诊断只允许通过命令行启动，不增加桌面启动图标。
const readingDiagAbility = {
  ...shareDiagAbility,
  name: 'ReadingDiagAbility',
  srcEntry: './ets/syncdiag/ReadingDiagAbility.ets',
  skills: [],
};

function shareSpikePlugin(): HvigorPlugin {
  return {
    pluginId: 'guizhiShareSpike',
    apply(node: HvigorNode) {
      const hapContext = node.getContext(OhosPluginId.OHOS_HAP_PLUGIN) as OhosHapContext;
      hapContext.targets((target) => {
        if (target.getCurrentProduct().getProductName() !== SHARE_SPIKE_PRODUCT) return;
        const moduleJson = hapContext.getModuleJsonOpt();
        moduleJson.module.abilities = (moduleJson.module.abilities ?? []).concat([shareDiagAbility, readingDiagAbility]);
        moduleJson.module.extensionAbilities = (moduleJson.module.extensionAbilities ?? []).concat([shareDiagExtension]);
        hapContext.setModuleJsonOpt(moduleJson);
      });
    },
  };
}

export default { system: hapTasks, plugins: [shareSpikePlugin()] };
