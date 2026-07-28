import { globalShortcut, BrowserWindow, ipcMain, app } from 'electron';
import path from 'path';
import fs from 'fs';
import { getConfigDir } from './runtime-paths';

// Shortcut config storage path
// 快捷键配置存储路径
const getLegacyShortcutsPath = () => path.join(app.getPath('userData'), 'shortcuts.json');
const getLegacyShortcutModePath = () => path.join(app.getPath('userData'), 'shortcut-mode.json');
const getConfiguredShortcutsPath = () => path.join(getConfigDir(), 'shortcuts.json');
const getConfiguredShortcutModePath = () =>
  path.join(getConfigDir(), 'shortcut-mode.json');
const getShortcutsPath = () => {
  const configPath = getConfiguredShortcutsPath();
  return fs.existsSync(configPath) ? configPath : getLegacyShortcutsPath();
};
const getShortcutModePath = () => {
  const configPath = getConfiguredShortcutModePath();
  return fs.existsSync(configPath) ? configPath : getLegacyShortcutModePath();
};

// Default shortcut configuration
// 默认快捷键配置
// Notes: prefer uncommon combos to avoid conflicts with system/common shortcuts
// 注意：使用不常用的组合键，避免与系统和常用应用冲突
// - Avoid Cmd/Ctrl+N (new), Cmd/Ctrl+F (find), Cmd/Ctrl+, (settings), etc.
// - 避免 Cmd/Ctrl+N (新建)、Cmd/Ctrl+F (搜索)、Cmd/Ctrl+, (设置) 等常用快捷键
// - Use Alt/Option combos to reduce conflicts
// - 使用 Alt/Option 组合键更不容易冲突
const DEFAULT_SHORTCUTS: Record<string, string> = {
  showApp: 'Alt+Shift+P',           // Show/hide app
                                   // 显示/隐藏应用
  newItem: 'Alt+Shift+N',           // Create new knowledge item (quick capture)
                                   // 新建知识条目（快速采集）
  search: 'Alt+Shift+F',            // Search
                                   // 搜索
  settings: 'Alt+Shift+S',          // Open settings
                                   // 打开设置
};
const DEFAULT_SHORTCUT_MODES: Record<string, 'global' | 'local'> = {
  showApp: 'global',
  newItem: 'local',
  search: 'local',
  settings: 'local',
};

// 旧版本配置里「新建」动作叫 newPrompt（PromptHub 时代命名），读取时迁移为 newItem
function migrateLegacyActionKey<T>(config: Record<string, T>): Record<string, T> {
  if ('newPrompt' in config) {
    if (!('newItem' in config)) {
      config.newItem = config.newPrompt;
    }
    delete config.newPrompt;
  }
  return config;
}

// Current shortcut configuration
// 当前快捷键配置
let currentShortcuts: Record<string, string> = { ...DEFAULT_SHORTCUTS };

// Current shortcut modes: 'global' (system-wide) or 'local' (only when app is focused)
// 当前快捷键模式：'global' (全局) 或 'local' (仅在应用聚焦时)
// Default: showApp is global, others are local to avoid conflicts
// 默认：showApp 为全局，其他为局部以避免冲突
let shortcutModes: Record<string, 'global' | 'local'> = {
  ...DEFAULT_SHORTCUT_MODES,
};

type ShortcutWindow = Pick<
  BrowserWindow,
  'isMinimized' | 'restore' | 'isVisible' | 'show' | 'hide' | 'focus'
>;

export function toggleWindowForShowApp(
  win: ShortcutWindow,
  onVisibilityChange?: (isVisible: boolean) => void,
): void {
  if (win.isMinimized()) {
    win.restore();
    win.show();
    win.focus();
    onVisibilityChange?.(true);
    return;
  }

  if (win.isVisible()) {
    win.hide();
    onVisibilityChange?.(false);
    return;
  }

  win.show();
  win.focus();
  onVisibilityChange?.(true);
}

/**
 * Load shortcut configuration
 * 加载快捷键配置
 */
function loadShortcuts(): Record<string, string> {
  try {
    const shortcutsPath = getShortcutsPath();
    if (fs.existsSync(shortcutsPath)) {
      const data = fs.readFileSync(shortcutsPath, 'utf-8');
      const saved = migrateLegacyActionKey(
        JSON.parse(data) as Record<string, string>,
      );
      return { ...DEFAULT_SHORTCUTS, ...saved };
    }
  } catch (error) {
    console.error('Failed to load shortcuts:', error);
  }
  return { ...DEFAULT_SHORTCUTS };
}

/**
 * Load shortcut modes
 * 加载快捷键模式
 */
function loadShortcutModes(): Record<string, 'global' | 'local'> {
  try {
    const modePath = getShortcutModePath();
    if (fs.existsSync(modePath)) {
      const data = fs.readFileSync(modePath, 'utf-8');
      const parsed = JSON.parse(data);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        migrateLegacyActionKey(parsed as Record<string, unknown>);
      }
      return normalizeShortcutModes(parsed);
    }
  } catch (error) {
    console.error('Failed to load shortcut modes:', error);
  }
  return { ...DEFAULT_SHORTCUT_MODES };
}

function normalizeShortcutModes(value: unknown): Record<string, 'global' | 'local'> {
  const normalized: Record<string, 'global' | 'local'> = {
    ...DEFAULT_SHORTCUT_MODES,
  };

  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return normalized;
  }

  const input = value as Record<string, unknown>;
  for (const action of Object.keys(DEFAULT_SHORTCUT_MODES)) {
    const mode = input[action];
    if (mode === 'global' || mode === 'local') {
      normalized[action] = mode;
    }
  }

  return normalized;
}

/**
 * Save shortcut configuration
 * 保存快捷键配置
 */
function saveShortcuts(shortcuts: Record<string, string>): boolean {
  try {
    const shortcutsPath = getConfiguredShortcutsPath();
    fs.mkdirSync(path.dirname(shortcutsPath), { recursive: true });
    fs.writeFileSync(shortcutsPath, JSON.stringify(shortcuts, null, 2));
    return true;
  } catch (error) {
    console.error('Failed to save shortcuts:', error);
    return false;
  }
}

/**
 * Save shortcut modes
 * 保存快捷键模式
 */
function saveShortcutModes(modes: Record<string, 'global' | 'local'>): boolean {
  try {
    const modePath = getConfiguredShortcutModePath();
    fs.mkdirSync(path.dirname(modePath), { recursive: true });
    fs.writeFileSync(modePath, JSON.stringify(modes, null, 2));
    return true;
  } catch (error) {
    console.error('Failed to save shortcut modes:', error);
    return false;
  }
}

/**
 * 导入配置时落盘快捷键。
 *
 * 只认 DEFAULT_SHORTCUTS 里已有的动作名——文件是外来的，不该由它决定注册哪些
 * 动作。这里不重新注册：导入完成后应用会重启，registerShortcuts 届时从文件
 * 重新加载，此刻注册纯属多余。写失败直接抛出，导入是用户主动点的，
 * 「快捷键没导进来」必须说得出原因。
 */
export function persistImportedShortcuts(
  accelerators: Record<string, string>,
  modes: Record<string, 'global' | 'local'>,
): void {
  const incoming = migrateLegacyActionKey({ ...accelerators });
  const nextShortcuts: Record<string, string> = { ...DEFAULT_SHORTCUTS };
  for (const action of Object.keys(DEFAULT_SHORTCUTS)) {
    // 空串是「用户清掉了这个快捷键」，与「文件里没写」不同，要保留
    if (typeof incoming[action] === 'string') {
      nextShortcuts[action] = incoming[action];
    }
  }
  const nextModes = normalizeShortcutModes(migrateLegacyActionKey({ ...modes }));

  const shortcutsPath = getConfiguredShortcutsPath();
  fs.mkdirSync(path.dirname(shortcutsPath), { recursive: true });
  fs.writeFileSync(shortcutsPath, JSON.stringify(nextShortcuts, null, 2));

  const modePath = getConfiguredShortcutModePath();
  fs.mkdirSync(path.dirname(modePath), { recursive: true });
  fs.writeFileSync(modePath, JSON.stringify(nextModes, null, 2));

  currentShortcuts = nextShortcuts;
  shortcutModes = nextModes;
}

/**
 * Register a single global shortcut
 * 注册单个全局快捷键
 */
function registerSingleShortcut(action: string, accelerator: string): boolean {
  if (!accelerator) return false;
  
  try {
    const success = globalShortcut.register(accelerator, () => {
      const windows = BrowserWindow.getAllWindows();
      if (windows.length > 0) {
        const win = windows[0];
        
        // For show-app shortcut: toggle window visibility
        // 如果是显示应用快捷键，切换窗口显示状态
        if (action === 'showApp') {
          toggleWindowForShowApp(win, (isVisible) => {
            win.webContents.send('window:visibility-changed', isVisible);
          });
        }
        
        // Send shortcut event to renderer
        // 发送快捷键触发事件到渲染进程
        win.webContents.send('shortcut:triggered', action);
      }
    });
    
    if (!success) {
      console.warn(`Failed to register shortcut: ${accelerator} for action: ${action}`);
    }
    return success;
  } catch (error) {
    console.error(`Error registering shortcut ${accelerator}:`, error);
    return false;
  }
}

/**
 * Register all global shortcuts (only if mode is 'global' for that shortcut)
 * 注册全局快捷键（仅当该快捷键模式为 'global' 时）
 */
export function registerShortcuts(
  options: { skipGlobal?: boolean } = {},
): void {
  // Load saved shortcut configuration
  // 加载保存的快捷键配置
  currentShortcuts = loadShortcuts();
  shortcutModes = loadShortcutModes();
  
  // Unregister all existing shortcuts
  // 注销所有现有快捷键
  globalShortcut.unregisterAll();

  // 自动化实例与用户正在运行的归知并存（E2E 绕过了单实例门），抢注系统级快捷键
  // 会把用户那个 Alt+Shift+P 夺走，且注册失败只打一条 warn，两边都发现不了。
  // 配置照常加载，设置页里的快捷键列表仍要显示得出来。
  if (options.skipGlobal) {
    return;
  }
  
  // Register each shortcut
  // 注册每个快捷键
  for (const [action, accelerator] of Object.entries(currentShortcuts)) {
    if (accelerator) {
      // Check mode for this shortcut
      // 检查该快捷键的模式
      const mode = shortcutModes[action] || 'local'; // Default to local for safety / 默认为 local
      if (mode === 'global') {
        registerSingleShortcut(action, accelerator);
      }
    }
  }
}

/**
 * Unregister all global shortcuts
 * 注销所有全局快捷键
 */
export function unregisterShortcuts(): void {
  globalShortcut.unregisterAll();
}

/**
 * Send shortcut event to renderer process
 * 发送快捷键事件到渲染进程
 */
export function sendShortcutToRenderer(channel: string): void {
  const win = BrowserWindow.getFocusedWindow();
  if (win) {
    win.webContents.send(channel);
  }
}

/**
 * Get current shortcut modes
 * 获取当前快捷键模式
 */
export function getShortcutModes(): Record<string, 'global' | 'local'> {
  return shortcutModes;
}

/**
 * Get current shortcuts configuration
 * 获取当前快捷键配置
 */
export function getCurrentShortcuts(): Record<string, string> {
  return currentShortcuts;
}

/**
 * Register shortcut-related IPC handlers
 * 注册快捷键相关的 IPC 处理程序
 */
export function registerShortcutsIPC(): void {
  // Get shortcut configuration
  // 获取快捷键配置
  ipcMain.handle('shortcuts:get', () => {
    return currentShortcuts;
  });

  // Set shortcut configuration
  // 设置快捷键配置
  ipcMain.handle('shortcuts:set', (_event, shortcuts: Record<string, string>) => {
    currentShortcuts = shortcuts;
    const saved = saveShortcuts(shortcuts);
    
    // Re-register shortcuts (only if global mode)
    // 重新注册快捷键（仅当全局模式时）
    globalShortcut.unregisterAll();
    
    for (const [action, accelerator] of Object.entries(shortcuts)) {
      if (accelerator) {
        const mode = shortcutModes[action] || 'local';
        if (mode === 'global') {
          registerSingleShortcut(action, accelerator);
        }
      }
    }

    // Broadcast update to all windows (for local shortcut handling)
    // 广播更新给所有窗口（用于局部快捷键处理）
    BrowserWindow.getAllWindows().forEach(win => {
      win.webContents.send('shortcuts:updated', shortcuts);
    });
    
    return saved;
  });

  // Set shortcut modes
  // 设置快捷键模式
  ipcMain.on('shortcuts:setMode', (_event, modes: Record<string, 'global' | 'local'>) => {
    shortcutModes = normalizeShortcutModes(modes);
    saveShortcutModes(shortcutModes);
    
    // Re-register shortcuts based on mode
    // 根据模式重新注册快捷键
    globalShortcut.unregisterAll();
    
    for (const [action, accelerator] of Object.entries(currentShortcuts)) {
      if (accelerator) {
        const mode = shortcutModes[action] || 'local';
        if (mode === 'global') {
          registerSingleShortcut(action, accelerator);
        }
      }
    }
  });

  // Get shortcut modes
  // 获取快捷键模式
  ipcMain.handle('shortcuts:getMode', () => {
    return shortcutModes;
  });
}
