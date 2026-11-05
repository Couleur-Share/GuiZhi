import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { util } from '@kit.ArkTS';
import { ReadingIdentity, READING_MAX_BYTES, readingProtectedHeader, splitReadingEnvelope } from '../core/ReadingSyncFormat';

const encoder = new util.TextEncoder();
const codec = new util.Base64Helper();

function encode(bytes: Uint8Array): string {
  return codec.encodeToStringSync(bytes).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function decode(text: string): Uint8Array {
  const padded = text.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - text.length % 4) % 4);
  const bytes = codec.decodeSync(padded);
  if (encode(bytes) !== text) throw new Error('阅读同步编码非规范');
  return bytes;
}

function assertKey(key: Uint8Array): void {
  if (key.length !== 32) throw new Error('阅读密钥必须为 256 位');
}

export class ReadingCrypto {
  encode(bytes: Uint8Array): string { return encode(bytes); }
  decode(text: string): Uint8Array { return decode(text); }
  bytes(text: string): Uint8Array { return encoder.encodeInto(text); }
  text(bytes: Uint8Array): string { return new util.TextDecoder('utf-8', { fatal: true }).decodeToString(bytes); }
  async hash(text: string): Promise<string> {
    const md = cryptoFramework.createMd('SHA256');
    await md.update({ data: this.bytes(text) });
    const result = await md.digest();
    return Array.from(result.data).map((value: number): string => value.toString(16).padStart(2, '0')).join('');
  }
  async hmac(key: Uint8Array, text: string): Promise<string> {
    const symKey = await cryptoFramework.createSymKeyGenerator('HMAC').convertKey({ data: key });
    try {
      const mac = cryptoFramework.createMac('SHA256');
      await mac.init(symKey); await mac.update({ data: this.bytes(text) });
      const result = await mac.doFinal();
      return Array.from(result.data).map((value: number): string => value.toString(16).padStart(2, '0')).join('');
    } finally { symKey.clearMem(); }
  }
  async derive(secret: Uint8Array, salt: string, info: string): Promise<Uint8Array> {
    const spec: cryptoFramework.HKDFSpec = { algName: 'HKDF', key: secret, salt: this.bytes(salt), info: this.bytes(info), keySize: 32 };
    return (await cryptoFramework.createKdf('HKDF|SHA256').generateSecret(spec)).data;
  }
  generateKey(): Uint8Array {
    return cryptoFramework.createRandom().generateRandomSync(32).data;
  }

  async encrypt(key: Uint8Array, identity: ReadingIdentity, plain: Uint8Array): Promise<string> {
    assertKey(key);
    if (plain.length < 1 || plain.length > READING_MAX_BYTES) throw new Error('阅读同步对象为空或超限');
    const header = encode(encoder.encodeInto(readingProtectedHeader(identity)));
    const iv = cryptoFramework.createRandom().generateRandomSync(12).data;
    const params: cryptoFramework.GcmParamsSpec = {
      algName: 'GcmParamsSpec', iv: { data: iv }, aad: { data: encoder.encodeInto(header) },
      authTag: { data: new Uint8Array(16) },
    };
    const symKey = await cryptoFramework.createSymKeyGenerator('AES256').convertKey({ data: key });
    try {
      const cipher = cryptoFramework.createCipher('AES256|GCM|NoPadding');
      await cipher.init(cryptoFramework.CryptoMode.ENCRYPT_MODE, symKey, params);
      // 鸿蒙 doFinal 单次调用返回密文及末尾 16 字节认证标签。
      const result = await cipher.doFinal({ data: plain });
      if (!result || result.data.length !== plain.length + 16) throw new Error('系统加密返回长度无效');
      const bytes = result.data;
      return [header, '', encode(iv), encode(bytes.slice(0, bytes.length - 16)), encode(bytes.slice(bytes.length - 16))].join('.');
    } finally { symKey.clearMem(); }
  }

  async decrypt(key: Uint8Array, expected: ReadingIdentity, token: string): Promise<Uint8Array> {
    assertKey(key);
    const parts = splitReadingEnvelope(token);
    const header = encode(encoder.encodeInto(readingProtectedHeader(expected)));
    if (parts[0] !== header) throw new Error('阅读同步身份或协议不匹配');
    const iv = decode(parts[2]), cipherText = decode(parts[3]), tag = decode(parts[4]);
    if (cipherText.length > READING_MAX_BYTES) throw new Error('阅读同步对象超限');
    const params: cryptoFramework.GcmParamsSpec = {
      algName: 'GcmParamsSpec', iv: { data: iv }, aad: { data: encoder.encodeInto(header) }, authTag: { data: tag },
    };
    const symKey = await cryptoFramework.createSymKeyGenerator('AES256').convertKey({ data: key });
    try {
      const cipher = cryptoFramework.createCipher('AES256|GCM|NoPadding');
      await cipher.init(cryptoFramework.CryptoMode.DECRYPT_MODE, symKey, params);
      // 不使用 update 暴露未经认证的明文；doFinal 成功才允许调用者落盘。
      const result = await cipher.doFinal({ data: cipherText });
      if (!result || result.data.length !== cipherText.length) throw new Error('系统解密返回长度无效');
      return result.data;
    } catch {
      throw new Error('阅读同步认证失败，内容或密钥不匹配');
    } finally { symKey.clearMem(); }
  }
}
