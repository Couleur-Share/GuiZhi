import { common } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { LocalState, Draft } from '../core/Protocol';
import { StateStore } from '../core/DraftQueue';

export class LocalStore implements StateStore {
  private prefs: preferences.Preferences;
  constructor(prefs: preferences.Preferences) { this.prefs = prefs; }

  static async open(context: common.Context): Promise<LocalStore> {
    const prefs = await preferences.getPreferences(context, { name: 'guizhi_capture_v1' });
    return new LocalStore(prefs);
  }

  async load(): Promise<LocalState> {
    const raw = await this.prefs.get('state', '') as string;
    if (!raw) return new LocalState();
    const state = JSON.parse(raw) as LocalState;
    if (state.version !== 1 || typeof state.editor !== 'string' || !Array.isArray(state.drafts) ||
      !['auto', 'urls', 'text'].includes(state.mode)) throw new Error('本机草稿格式无法读取，请保留应用数据并联系开发者');
    for (let i = 0; i < state.drafts.length; i++) {
      const draft: Draft = state.drafts[i];
      if (!draft.requestId || typeof draft.input !== 'string' || typeof draft.binding !== 'string' ||
        typeof draft.attempted !== 'boolean' || !['auto', 'urls', 'text'].includes(draft.mode)) {
        throw new Error('本机草稿校验失败，已停止写入以保留原数据');
      }
    }
    return state;
  }

  async save(state: LocalState): Promise<void> {
    await this.prefs.put('state', JSON.stringify(state));
    await this.prefs.flush();
  }
}
