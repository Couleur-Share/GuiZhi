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
  IMPORT_CLEAR_FINISHED: "import:clearFinished",
  /** 主进程 → 渲染进程：任务状态变更事件 */
  IMPORT_CHANGED: "import:changed",

  // Wiki (ADR 0023)
  WIKI_CATALOG: "wiki:catalog",
  WIKI_GET_PAGE: "wiki:getPage",
  WIKI_APPLY_COMPILATION: "wiki:applyCompilation",
  WIKI_LIST_COMPILABLE: "wiki:listCompilable",
  WIKI_LIST_INGESTIONS: "wiki:listIngestions",
  WIKI_STATUS: "wiki:status",
  WIKI_CLEAR: "wiki:clear",
  WIKI_GRAPH: "wiki:graph",

  // Legacy data migration (one-time, from .NET GuiZhi)
  MIGRATION_DETECT_LEGACY: "migration:detectLegacy",
  MIGRATION_RUN_LEGACY: "migration:runLegacy",

  // Local backup / restore / export
  BACKUP_CREATE: "backup:create",
  BACKUP_LIST: "backup:list",
  BACKUP_DELETE: "backup:delete",
  BACKUP_RESTORE: "backup:restore",
  EXPORT_MARKDOWN: "export:markdown",

  // Ask sessions (AI 问答会话持久化)
  ASK_SESSION_LIST: "ask:listSessions",
  ASK_SESSION_GET: "ask:getSession",
  ASK_SESSION_SAVE: "ask:saveSession",
  ASK_SESSION_DELETE: "ask:deleteSession",

  // Media enrichment (音视频转写)
  MEDIA_TRANSCRIBE: "media:transcribe",
  /** 已有文字稿的 AI 排版（补标点/分段，不重新转写） */
  MEDIA_FORMAT_TRANSCRIPT: "media:formatTranscript",
  /** 基于文字稿生成结构化「视频/音频总结」并写入正文 */
  MEDIA_SUMMARIZE: "media:summarize",
  /** 转写模型连通性测试（静音样本真实请求） */
  MEDIA_TEST_TRANSCRIPTION: "media:testTranscription",

  // yt-dlp 工具管理（在线视频解析引擎）
  YTDLP_STATUS: "ytdlp:status",
  YTDLP_INSTALL: "ytdlp:install",
  YTDLP_REMOVE: "ytdlp:remove",
  YTDLP_PICK_BINARY: "ytdlp:pickBinary",
  /** 主进程 → 渲染进程：安装下载进度 */
  YTDLP_DOWNLOAD_PROGRESS: "ytdlp:downloadProgress",

  // ffmpeg 工具管理（转写前音频转码引擎）
  FFMPEG_STATUS: "ffmpeg:status",
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

  // AI HTTP proxy (bypass CORS via main process)
  AI_HTTP_REQUEST: "ai:httpRequest",
  AI_HTTP_STREAM: "ai:httpStream",
  AI_HTTP_STREAM_CHUNK: "ai:httpStreamChunk",
  AI_HTTP_STREAM_ERROR: "ai:httpStreamError",
  AI_HTTP_STREAM_CANCEL: "ai:httpStreamCancel",

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
