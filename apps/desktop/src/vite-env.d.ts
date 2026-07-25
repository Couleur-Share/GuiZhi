/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly DEV: boolean;
  readonly PROD: boolean;
  readonly MODE: string;
  readonly VITE_GUIZHI_CLOUD_ENABLED?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
