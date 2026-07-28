/**
 * 配置迁移文件的机密加解密。
 *
 * 只加密机密字段而不是整份文件：这样导出的 JSON 仍然打得开、看得懂——里面有
 * 哪些服务商、哪些模型、什么路由一目了然，只有 apiKey 与代理密码是 `ENC::` 密文。
 * 忘了密码也不是灾难，其余配置照常导入，手填几个 Key 就能用。
 *
 * 算法复用 `main/security.ts` 的 AES-256-GCM 实现（在此之前它零调用方）。
 */
import crypto from "crypto";
import type { ConfigTransferFile } from "@guizhi/shared/types";
import { CONFIG_TRANSFER_CANARY } from "@guizhi/shared/types";
import { mapConfigSecrets } from "@guizhi/shared/utils/config-transfer";
import { decryptText, encryptText } from "../../security";

/**
 * scrypt 代价参数。N=16384 / r=8 约占 16MB 内存、单次派生百毫秒量级；
 * 导出与导入各只派生一次，这个开销换来的是离线爆破成本。
 */
const SCRYPT_N = 16384;
const SCRYPT_R = 8;
const SCRYPT_P = 1;

/**
 * scrypt 默认 maxmem 只有 32MB，够当前参数但卡在临界；参数是随文件走的
 * （shared 的信封校验已把上界卡在 N=65536 / r=8），这里按那个上界留足。
 */
const SCRYPT_MAXMEM = 128 * 1024 * 1024;

function deriveKey(
  password: string,
  salt: Buffer,
  params: { n: number; r: number; p: number },
): Buffer {
  return crypto.scryptSync(password, salt, 32, {
    N: params.n,
    r: params.r,
    p: params.p,
    maxmem: SCRYPT_MAXMEM,
  });
}

/** 就地加密文件里的全部机密字段，并写入自描述的加密块 */
export function encryptConfigSecrets(
  file: ConfigTransferFile,
  password: string,
): void {
  const salt = crypto.randomBytes(16);
  const params = { n: SCRYPT_N, r: SCRYPT_R, p: SCRYPT_P };
  const key = deriveKey(password, salt, params);

  file.encryption = {
    algo: "aes-256-gcm",
    kdf: "scrypt",
    salt: salt.toString("base64"),
    n: params.n,
    r: params.r,
    p: params.p,
    canary: encryptText(CONFIG_TRANSFER_CANARY, key),
  };
  mapConfigSecrets(file, (value) => encryptText(value, key));
}

export interface ConfigDecryptResult {
  ok: boolean;
  /** 密码错：用户改一下就能过，与「文件坏了」要分开说 */
  wrongPassword?: boolean;
  /** 解不开的机密字段数（密码对但个别字段损坏时才会非零） */
  failed?: number;
  error?: string;
}

/**
 * 就地解密文件里的机密字段。
 *
 * 先解 canary 再动真正的字段：密码错的时候当场说清楚，而不是让一堆解不开的
 * 密文被当成 Key 写进配置——那种失败要到用户下一次调模型时才暴露。
 */
export function decryptConfigSecrets(
  file: ConfigTransferFile,
  password: string,
): ConfigDecryptResult {
  const encryption = file.encryption;
  if (!encryption) {
    return { ok: true };
  }
  if (!password) {
    return { ok: false, wrongPassword: true, error: "这份配置需要密码才能打开" };
  }

  let key: Buffer;
  try {
    key = deriveKey(password, Buffer.from(encryption.salt, "base64"), {
      n: encryption.n,
      r: encryption.r,
      p: encryption.p,
    });
  } catch (error) {
    return {
      ok: false,
      error: `无法按文件里的参数派生密钥：${error instanceof Error ? error.message : String(error)}`,
    };
  }

  if (decryptText(encryption.canary, key) !== CONFIG_TRANSFER_CANARY) {
    return { ok: false, wrongPassword: true, error: "密码不正确" };
  }

  const stats = mapConfigSecrets(file, (value) => decryptText(value, key));
  delete file.encryption;
  return { ok: true, failed: stats.failed };
}
