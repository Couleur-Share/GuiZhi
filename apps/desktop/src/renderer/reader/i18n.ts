import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "../i18n/locales/en.json";
import zh from "../i18n/locales/zh.json";
import type { ReaderLang } from "./protocol";

/**
 * 岛里的 i18n：与渲染层 i18n/index.ts 同一套选项与同一份语言包（两种语言都内置，岛里没有网络，也没有按需加载）。
 * 复用的阅读组件照常用 useTranslation / t(key, 兜底文案)，不需要为岛另写一份文案。
 */
export function initReaderI18n(lang: ReaderLang): Promise<unknown> {
  if (i18n.isInitialized) return i18n.changeLanguage(lang);
  return i18n.use(initReactI18next).init({
    resources: { en: { translation: en }, zh: { translation: zh } },
    lng: lang,
    fallbackLng: "en",
    interpolation: { escapeValue: false },
    // 资源是内联的：同步完成初始化，第一次渲染就拿得到文案。
    initAsync: false,
  });
}
