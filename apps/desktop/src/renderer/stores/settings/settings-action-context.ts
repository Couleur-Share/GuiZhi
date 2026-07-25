import type { Settings } from "@guizhi/shared/types";
import type { StoreApi } from "zustand";
import type { SettingsState } from "./settings-types";

export interface SettingsActionContext {
  set: StoreApi<SettingsState>["setState"];
  get: StoreApi<SettingsState>["getState"];
  setTouched: (partial: Partial<SettingsState>) => void;
  commitAISettings: (partial: Partial<SettingsState>) => void;
  syncSettingsToMain: (settings: Partial<Settings>) => Promise<void>;
}

export type ActionKeys = {
  [Key in keyof SettingsState]: SettingsState[Key] extends (
    ...args: never[]
  ) => unknown
    ? Key
    : never;
}[keyof SettingsState];

export type SettingsActionGroup<Key extends ActionKeys> = Pick<
  SettingsState,
  Key
>;
