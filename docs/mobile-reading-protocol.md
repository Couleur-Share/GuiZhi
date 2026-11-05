# 阅读同步 v1：密码协议与验证

状态：阶段 1 密码基础和阶段 2 桌面/服务接线已完成本地合成验证；尚未上传真实资料、部署或交付手机阅读界面。网络与配对协议见[阶段 2 接线](./mobile-reading-phase2.md)。

## 标准与身份

采用 [RFC 7516 JWE Compact](https://www.rfc-editor.org/rfc/rfc7516.html) 的直接加密模式，算法固定为 `dir` / `A256GCM`，参数按 [RFC 7518](https://www.rfc-editor.org/rfc/rfc7518.html)：32 字节密钥、12 字节随机 IV、16 字节认证标签。鸿蒙适配依据 [OpenHarmony Crypto Framework GCM 文档](https://github.com/openharmony/docs/blob/master/en/application-dev/security/CryptoArchitectureKit/crypto-aes-sym-encrypt-decrypt-gcm.md) 及本机 SDK 声明，使用 `AES256|GCM|NoPadding`。SDK 编译不证明实际设备行为。

每个 token 为 `protected..iv.ciphertext.tag`；各部分使用无填充的规范 base64url。受保护头的编码文本作为 AAD；固定头字段顺序用于限制本 profile，拒绝额外字段、非规范编码和算法变化。不是通用 JWE 解析器。

| 受保护字段 | 语义 |
| --- | --- |
| alg / enc / typ / v | `dir` / `A256GCM` / `guizhi-reading+jwe` / `1` |
| libraryId / generation | 不透明库 ID 与世代；禁止使用知识库名称或用户身份 |
| objectId / keyId | 同步对象与内容密钥标识；不可使用磁盘路径 |
| revision | 正整数版本，不能超过 JS 安全整数上限 |
| kind | `article`、`image`、`manifest`、`recovery`、`pairing` |

解密必须传入可信的期望身份，不能从收到的 token 中取出身份再当作校验标准。阶段 2 的可信配对包携带库密钥及清单检查点；对象身份来自已认证清单。阶段 3 必须将该检查点接入手机首次下载，没有可信首次绑定时不能宣称抵抗服务端密钥替换或首次回滚。

密钥生成及 IV 使用平台安全随机源。业务 API 不开放固定 IV；重试复用已持久化密文，不能重新生成相同版本对象。生产接线前还需确定每个密钥的加密次数限制与轮换流程。

## 阅读投影与完整清单

投影显式选择标题、正文、摘要、类型、状态、分类名称、标签、安全来源链接和同步 ID；不展开数据库整行。来源链接只保留 HTTPS、不带用户名密码，并移除 query 与 fragment。正文是用户提供的内容，不承诺自动识别或清除正文中的秘密。

投影基础接收已选好的正文；阶段 2 桌面 `reading-sync/source.ts` 已接入数据库事务读取，使用当前编辑正文、摘要与白名单关系，将受支持的本地图片引用转换为同步对象 ID。`access_uri`、Cookie、API Key 和源数据库 ID 都不在投影白名单内。

首版清单是完整对象目录 `mode=snapshot`，正文和图片分别加密。每条目录项记录对象 ID、版本、用途、密钥 ID、密文字节数与 SHA-256；目录自身也加密。完整目录中缺失的原对象表示撤回。对象 ID 不得与清单 ID 相同，不得重复。

组装器先生成全部对象，再生成清单；下载验证器先认证清单，再逐项校验大小、哈希、身份与认证标签。全部成功才返回，失败时清除本轮已解密的待提交缓冲。后续本地数据库必须在一个事务中切换可见数据和检查点，当前代码不提供数据库提交。

检查点保存库 ID、世代、单调序号和清单哈希。同版本同哈希为幂等重放；同版本不同哈希、版本倒退、跨库或世代变化均拒绝。检查点规则须在正式下载编排中接线，目前只提供独立验证函数。

阶段 1 的保护上限：单明文对象 8 MiB、投影正文 4 MiB、清单 10,000 项、有界内存快照 64 MiB。上限用于防止失控分配，不是产品套餐或最终容量承诺。大库要改用受限暂存区和分页清单，不能直接提高限制。

## 密钥恢复与手机存储

恢复密钥为独立生成的 32 字节随机秘密，文本形式 `guizhi-rk1.<base64url>`，不接受人类口令替代。内容密钥以 `kind=recovery` 的 JWE 封装，恢复时仍需校验期望库、世代、对象和 keyId。当前完成密码封装与恢复测试、专门的恢复材料显示操作；尚未实现账户恢复、恢复执行界面或服务端托管恢复材料。

鸿蒙 `ReadingKeyStore` 使用独立的 `guizhi.reading.key.v1.*` Asset Store 别名，设置设备解锁后可访问及禁止同步；相同 keyId 冲突不覆盖，失败不回落普通文件。此实现只保护内容密钥，手机阅读数据库和搜索索引的静态加密仍待后续实现。

内容密钥持有者能够生成有效 AEAD 密文；阶段 2 服务已独立验证 PC 写权限，不能把“会解密”当成“允许上传”。配对密钥交换和 PC 撤销已实现，撤销后冻结上传；轮换和丢失 PC 后的独立撤销仍待阶段 4。

## 可重复执行的检查

在仓库根目录运行：

```powershell
node scripts/sync-reading-format.mjs
pnpm --filter @guizhi/capture-relay exec tsx ../../scripts/create-reading-fixtures.mts
pnpm --filter @guizhi/core test
node --test apps/capture-harmony/tests/reading-sync.test.cjs
pnpm --filter @guizhi/capture-relay exec tsx --test tests/reading-sync-format.test.ts
```

`packages/shared/utils/reading-sync-format.ts` 是协议纯逻辑的唯一来源，鸿蒙副本由上述脚本生成并由测试逐字核对。公开夹具由 WebCrypto 生成，含两篇虚构文章、一张图片和一个恢复封装。Node 密码实现与鸿蒙适配层的 Node 系统替身消费同一向量；这不是设备互操作结论。

## 真机诊断

诊断只注册到 `dev` 产品，包名 `com.couleurapp.guizhi.capture.dev`，入口 `ReadingDiagAbility`，不出现在正式包中。未连接服务器，不读取收集凭据或用户文章。测试密钥是公开夹具，禁止用于真实资料。

1. 按 [真机回路](../apps/capture-harmony/docs/device-loop.md) 配置独立 dev 签名并安装开发包；不得覆盖正式包。
2. 运行 `apps/capture-harmony/scripts/device.ps1 launch -Ability ReadingDiagAbility -ToolsRoot D:/Tools/HarmonyOS/26.0.0.851/command-line-tools`，在诊断页点“开始验证”。
3. 将“复制报告”的内容保存为本地文本文件，然后执行 `pnpm --filter @guizhi/capture-relay exec tsx ../../scripts/verify-reading-report.mts <报告的绝对路径>`。

诊断验证公开向量的解密、原生加密回读、错误密钥/篡改/版本错配拒绝，以及独立随机 Asset Store 别名的写入、读回和清理。报告中四个 `ROUNDTRIP` 密文再由 Node 解密核对；报告不含随机存储探针密钥。只有确认来自本次真实手机且校验通过的报告，才记录双端互操作通过。

后续仍需单独验收锁屏访问拒绝、系统重启后的持久性、阅读库静态保护、密钥恢复 UI 和真实文章离线阅读，不能把诊断通过等同于功能完成。
