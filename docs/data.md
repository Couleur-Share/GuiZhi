# 数据与备份

## 数据目录

```text
%APPDATA%/GuiZhi/            # Windows；Linux 为 ~/.config/GuiZhi
├─ data/
│  ├─ knowledge.db           # 主数据库（SQLite + FTS5 + 向量表）
│  ├─ guizhi.db              # 旧 .NET 版数据库（只读迁移源）
│  └─ assets/                # 导入的图片 / 音视频 / 附件
├─ config/
│  ├─ ai-models.json         # 服务商、模型与路由配置
│  ├─ illustration-styles.json
│  ├─ mcp.json               # MCP 可访问的知识库范围
│  └─ shortcuts.json
├─ backups/                  # knowledge-{manual|auto|pre-update|pre-restore}-*.db
├─ tools/                    # 按需安装的 yt-dlp / ffmpeg / FunASR
└─ logs/
```

数据根目录可以在「设置 → 数据」里迁到别处。便携调试可用环境变量 `GUIZHI_DATA_DIR` 覆盖。

## 备份与恢复

- **备份 / 恢复**：在线一致性快照，手动 + 定时；恢复前会再存一份当前数据
- **Markdown 导出**：每条一个 `.md` + YAML frontmatter，按知识库分文件夹

## 配置迁移

「设置 → 数据 → 配置迁移」：把全部软件设置导出成一个 JSON，在新设备上导入。

与备份的分工是：**备份装条目、配置装设置**。换机完整路径是两样都做一遍。

- API Key 等机密可选加密带走（只加密机密字段，其余配置仍可读）
- 不导出本机路径类设置（yt-dlp / ffmpeg 路径、背景图文件名、开机启动等）
- 导入前会把 config 目录快照到 `config/pre-import-<时间戳>/`（留最近 3 份），导错可手工回滚

## 旧版迁移

一键把 .NET 时代的 `guizhi.db` 全量迁入当前 `knowledge.db`。首次启动若检测到旧库会提示。

## 日志

后台任务与失败会写入 `logs/error.log`。「设置 → 数据 → 打开日志」是唯一出口。发 issue 求助时可附上相关片段（日志会抹掉主目录路径，但仍请检查是否含敏感信息）。
