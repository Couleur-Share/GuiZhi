import { BusinessError } from '@kit.BasicServicesKit';
import { motion } from '@kit.MultimodalAwarenessKit';

export class HoldingHandState {
  side: string = 'right';
  message: string = '等待握持识别';
}

// 只在前台订阅；握持数据仅驱动本机界面，不保存、不上传。
export class HoldingHandObserver {
  private listening: boolean = false;
  private active: boolean = false;
  private timer: number = -1;
  private pending: number = -1;
  private applied: number = -1;
  private publish: (state: HoldingHandState) => void;
  constructor(publish: (state: HoldingHandState) => void) { this.publish = publish; }

  private emit(side: string, message: string): void {
    const state = new HoldingHandState(); state.side = side; state.message = message; this.publish(state);
  }

  private changed = (status: motion.HoldingHandStatus): void => {
    if (!this.active || status === this.pending || status < 0 || status > 3) return;
    if (status === this.applied) {
      if (this.timer !== -1) clearTimeout(this.timer);
      this.timer = -1; this.pending = -1; return;
    }
    this.pending = status;
    if (this.timer !== -1) clearTimeout(this.timer);
    // 等最后一次状态稳定后再移动，避免握持抖动让按钮在点击前来回跳。
    this.timer = setTimeout(() => {
      this.timer = -1;
      if (!this.active) return;
      this.applied = this.pending; this.pending = -1;
      if (this.applied === motion.HoldingHandStatus.LEFT_HAND_HELD) this.emit('left', '已识别左手 · 新建按钮靠左');
      else if (this.applied === motion.HoldingHandStatus.RIGHT_HAND_HELD) this.emit('right', '已识别右手 · 新建按钮靠右');
      else this.emit('right', this.applied === motion.HoldingHandStatus.BOTH_HANDS_HELD ? '双手握持 · 使用默认位置' : '未握持 · 使用默认位置');
    }, 280);
  };

  start(): void {
    if (this.active) return;
    if (!canIUse('SystemCapability.MultimodalAwareness.Motion')) {
      this.emit('right', '当前设备不支持握持识别'); return;
    }
    this.active = true; this.pending = -1; this.applied = -1;
    this.emit('right', '等待握持识别 · 尝试左右手自然握持手机');
    if (this.listening) return;
    try {
      motion.on('holdingHandChanged', this.changed);
      this.listening = true;
    } catch (error) {
      this.active = false;
      const code = (error as BusinessError).code;
      this.emit('right', code === 801 ? '当前设备不支持握持识别' :
        code === 201 ? '握持权限未生效，请重新安装新版后重试' : `握持识别暂不可用（${code || '未知错误'}），可关闭后重新开启`);
    }
  }

  stop(): void {
    this.active = false;
    if (this.timer !== -1) clearTimeout(this.timer);
    this.timer = -1; this.pending = -1; this.applied = -1;
    if (this.listening) {
      try { motion.off('holdingHandChanged', this.changed); this.listening = false; }
      catch { this.emit('right', '握持监听暂未释放，已停止响应；请重新打开应用'); }
    }
  }
}
