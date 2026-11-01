/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_GRAB_FARE_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
