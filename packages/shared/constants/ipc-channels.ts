/**
 * IPC channel definitions
 *
 * 所有跨进程频道的唯一出处：main 在 main/ipc/ 注册 handler，
 * preload 按白名单暴露，渲染进程经 window.api 调用。
 */

export const IPC_CHANNELS = {
  // Knowledge items
  KNOWLEDGE_LIST: "knowledge:list",
  KNOWLEDGE_GET: "knowledge:get",
  KNOWLEDGE_CREATE: "knowledge:create",
  KNOWLEDGE_UPDATE: "knowledge:update",
  KNOWLEDGE_BULK_UPDATE: "knowledge:bulkUpdate",
  KNOWLEDGE_SET_STATUS: "knowledge:setStatus",
  KNOWLEDGE_MOVE_TO_TRASH: "knowledge:moveToTrash",
  KNOWLEDGE_RESTORE: "knowledge:restore",
  KNOWLEDGE_DELETE_FOREVER: "knowledge:deleteForever",
  KNOWLEDGE_EMPTY_TRASH: "knowledge:emptyTrash",
  KNOWLEDGE_COUNTS: "knowledge:counts",

  // Collections
  COLLECTION_LIST: "collection:list",
  COLLECTION_CREATE: "collection:create",
  COLLECTION_UPDATE: "collection:update",
  COLLECTION_DELETE: "collection:delete",

  // Tags
  TAG_LIST: "tag:list",
  TAG_CREATE: "tag:create",
  TAG_UPDATE: "tag:update",
  TAG_DELETE: "tag:delete",

  // Import pipeline
  IMPORT_ENQUEUE: "import:enqueue",
  IMPORT_LIST: "import:list",
  IMPORT_CANCEL: "import:cancel",
  IMPORT_RETRY: "import:retry",
  /** 主进程 → 渲染进程：自动备份开始 / 结束（这段主线程是冻住的） */
  BACKUP_AUTO_STATUS: "backup:autoStatus",
  IMPORT_REMOVE: "import:remove",
  IMPORT_CLEAR_FINISHED: "import:clearFinished",
  /** 主进程 → 渲染进程：任务状态变更事件 */
  IMPORT_CHANGED: "import:changed",

  // Wiki (ADR 0023)
  WIKI_CATALOG: "wiki:catalog",
  /** 各页面入链数：目录列的「被引用最多」排序与孤立页筛选 */
  WIKI_BACKLINK_COUNTS: "wiki:backlinkCounts",
  WIKI_SEARCH: "wiki:search",
  WIKI_UPDATE_PAGE: "wiki:updatePage",
  WIKI_DELETE_PAGE: "wiki:deletePage",
  WIKI_GET_PAGE: "wiki:getPage",
  WIKI_APPLY_COMPILATION: "wiki:applyCompilation",
  WIKI_LIST_COMPILABLE: "wiki:listCompilable",
  WIKI_LIST_INGESTIONS: "wiki:listIngestions",
  WIKI_STATUS: "wiki:status",
  WIKI_CLEAR: "wiki:clear",
  WIKI_GRAPH: "wiki:graph",
  WIKI_RECORD_FAILURE: "wiki:recordCompilationFailure",
  WIKI_LIST_REVISIONS: "wiki:listRevisions",
  WIKI_RESTORE_REVISION: "wiki:restoreRevision",

  // Legacy data migration (one-time, from .NET GuiZhi)
  MIGRATION_DETECT_LEGACY: "migration:detectLegacy",
  MIGRATION_RUN_LEGACY: "migration:runLegacy",

  // Local backup / restore / export
  BACKUP_CREATE: "backup:create",
  BACKUP_LIST: "backup:list",
  BACKUP_DELETE: "backup:delete",
  BACKUP_RESTORE: "backup:restore",
  EXPORT_MARKDOWN: "export:markdown",
  /** 单条目的「AI 交接稿」另存为 .md（内容由渲染进程序列化好后传入） */
  EXPORT_AI_HANDOFF: "export:aiHandoff",

  /** 随包发的 stdio MCP server 的接入信息（供设置页生成客户端配置） */
  MCP_CONFIG: "mcp:config",
  /** MCP 可访问的知识库范围（读 / 写 config/mcp.json） */
  MCP_GET_SCOPE: "mcp:getScope",
  MCP_SET_SCOPE: "mcp:setScope",
  /** 一键安装：主进程拼好 deeplink 并交给系统打开 */
  MCP_INSTALL: "mcp:install",

  // 配置迁移（一键导入 / 导出全部软件设置，含加密后的 API Key）
  CONFIG_EXPORT: "config:export",
  CONFIG_READ: "config:read",
  CONFIG_APPLY: "config:apply",

  // Ask sessions (AI 问答会话持久化)
  ASK_SESSION_LIST: "ask:listSessions",
  ASK_SESSION_GET: "ask:getSession",
  ASK_SESSION_SAVE: "ask:saveSession",
  ASK_SESSION_DELETE: "ask:deleteSession",

  // Media enrichment (音视频转写)
  MEDIA_TRANSCRIBE: "media:transcribe",
  /** 已有文字稿的 AI 排版（补标点/分段，不重新转写） */
  MEDIA_FORMAT_TRANSCRIPT: "media:formatTranscript",
  /** 主进程 → 渲染进程：文字稿排版逐块进度 */
  MEDIA_FORMAT_PROGRESS: "media:formatProgress",
  /** 基于文字稿生成结构化「视频/音频总结」并写入正文 */
  MEDIA_SUMMARIZE: "media:summarize",
  /** 转写模型连通性测试（静音样本真实请求） */
  MEDIA_TEST_TRANSCRIPTION: "media:testTranscription",
  /** 当前「语音转写」路由支持哪些可选能力（如区分说话人） */
  MEDIA_CAPABILITIES: "media:capabilities",
  /** 主进程 → 渲染进程：转写进行中的已用时长与停滞时长 */
  MEDIA_TRANSCRIBE_PROGRESS: "media:transcribeProgress",

  // 正文配图（AI 文生图）
  /** 可用的插画风格预设 */
  ILLUSTRATION_STYLES: "illustration:styles",
  /** 应用内编辑器回写风格预设文件 */
  ILLUSTRATION_SAVE_STYLES: "illustration:saveStyles",
  /** 内置风格预设（编辑器的「恢复内置预设」用，不落盘） */
  ILLUSTRATION_BUILT_IN_STYLES: "illustration:builtInStyles",
  /** 在文件管理器里定位风格预设文件（不用默认程序打开，免得撞上应用选择框） */
  ILLUSTRATION_REVEAL_STYLES_FILE: "illustration:revealStylesFile",
  /** 读正文出配图规格（shot list），不生成图片 */
  ILLUSTRATION_PLAN: "illustration:plan",
  /** 按 shot list 逐张生成并写入正文 */
  ILLUSTRATION_GENERATE: "illustration:generate",
  /** 重新生成正文里已有的某一张（原位替换） */
  ILLUSTRATION_REGENERATE: "illustration:regenerate",
  /** 从正文移除某一张（磁盘资产随之回收） */
  ILLUSTRATION_REMOVE: "illustration:remove",
  /** 一次移除正文里的全部配图 */
  ILLUSTRATION_CLEAR: "illustration:clear",
  /** 文生图模型连通性测试（真实生成一张最小尺寸的图） */
  ILLUSTRATION_TEST: "illustration:testModel",
  /** 中断在途的配图生成 */
  ILLUSTRATION_CANCEL: "illustration:cancel",
  /** 主进程 → 渲染进程：逐张生成进度 */
  ILLUSTRATION_PROGRESS: "illustration:progress",

  // yt-dlp 工具管理（在线视频解析引擎）
  YTDLP_STATUS: "ytdlp:status",
  /** 只查远端版本号判断有无更新，不下载 */
  YTDLP_CHECK_UPDATE: "ytdlp:checkUpdate",
  YTDLP_INSTALL: "ytdlp:install",
  YTDLP_REMOVE: "ytdlp:remove",
  YTDLP_PICK_BINARY: "ytdlp:pickBinary",
  /** 主进程 → 渲染进程：安装下载进度 */
  YTDLP_DOWNLOAD_PROGRESS: "ytdlp:downloadProgress",

  // ffmpeg 工具管理（转写前音频转码引擎）
  FFMPEG_STATUS: "ffmpeg:status",
  /** 只读远端资产的构建日期判断有无新构建，不下载 */
  FFMPEG_CHECK_UPDATE: "ffmpeg:checkUpdate",
  FFMPEG_INSTALL: "ffmpeg:install",
  FFMPEG_REMOVE: "ffmpeg:remove",
  FFMPEG_PICK_BINARY: "ffmpeg:pickBinary",
  /** 主进程 → 渲染进程：安装下载进度 */
  FFMPEG_DOWNLOAD_PROGRESS: "ffmpeg:downloadProgress",

  // 本地转写引擎（托管 funasr-server + SenseVoiceSmall）
  FUNASR_STATUS: "funasr:status",
  FUNASR_INSTALL: "funasr:install",
  FUNASR_UNINSTALL: "funasr:uninstall",
  /** 主进程 → 渲染进程：安装阶段进度 */
  FUNASR_INSTALL_PROGRESS: "funasr:installProgress",

  // Semantic index (embedding 语义检索)
  SEMANTIC_STATUS: "semantic:status",
  SEMANTIC_LIST_PENDING: "semantic:listPending",
  SEMANTIC_APPLY_EMBEDDINGS: "semantic:applyEmbeddings",
  SEMANTIC_SEARCH: "semantic:search",
  SEMANTIC_CLEAR: "semantic:clear",

  // Settings
  SETTINGS_GET: "settings:get",
  SETTINGS_SET: "settings:set",

  // Security
  SECURITY_SET_MASTER_PASSWORD: "security:setMasterPassword",
  SECURITY_CHANGE_MASTER_PASSWORD: "security:changeMasterPassword",
  SECURITY_UNLOCK: "security:unlock",
  SECURITY_STATUS: "security:status",
  SECURITY_LOCK: "security:lock",

  // App runtime
  APP_RELAUNCH: "app:relaunch",
  APP_GET_CACHE_SIZE: "app:getCacheSize",
  APP_CLEAR_CACHE: "app:clearCache",
  APP_GET_RUNTIME_PATHS: "app:getRuntimePaths",
  APP_COMMAND: "app:command",
  /** 渲染进程 → 主进程：把业务失败记进 logs/error.log */
  LOG_APP_ERROR: "log:appError",

  // AI HTTP proxy (bypass CORS via main process)
  AI_HTTP_REQUEST: "ai:httpRequest",
  AI_HTTP_STREAM: "ai:httpStream",
  AI_HTTP_STREAM_CHUNK: "ai:httpStreamChunk",
  AI_HTTP_STREAM_ERROR: "ai:httpStreamError",
  /** 中断在途的 AI 请求（流式与非流式共用，按 requestId 定位） */
  AI_HTTP_CANCEL: "ai:httpCancel",
  AI_USAGE_RECORD: "ai:usageRecord",
  AI_USAGE_SUMMARY: "ai:usageSummary",
  AI_USAGE_CLEAR: "ai:usageClear",

  // Dialogs
  DIALOG_SELECT_IMAGE: "dialog:selectImage",
  DIALOG_SELECT_VIDEO: "dialog:selectVideo",

  // Images
  IMAGE_SAVE: "image:save",
  IMAGE_SAVE_BUFFER: "image:save-buffer",
  IMAGE_DOWNLOAD: "image:download",
  IMAGE_OPEN: "image:open",
  IMAGE_LIST: "image:list",
  IMAGE_GET_SIZE: "image:getSize",
  IMAGE_READ_BASE64: "image:readBase64",
  IMAGE_SAVE_BASE64: "image:saveBase64",
  IMAGE_EXISTS: "image:exists",
  IMAGE_CLEAR: "image:clear",

  // Videos
  VIDEO_SAVE: "video:save",
  VIDEO_OPEN: "video:open",
  VIDEO_LIST: "video:list",
  VIDEO_GET_SIZE: "video:getSize",
  VIDEO_READ_BASE64: "video:readBase64",
  VIDEO_SAVE_BASE64: "video:saveBase64",
  VIDEO_EXISTS: "video:exists",
  VIDEO_GET_PATH: "video:getPath",
  VIDEO_CLEAR: "video:clear",

  // Data path
  DATA_PREVIEW_RECOVERY: "data:previewRecovery",
} as const;

export type IpcChannel = (typeof IPC_CHANNELS)[keyof typeof IPC_CHANNELS];
