import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ScenarioDefaultsSection } from "../../../src/renderer/components/settings/ai-workbench/ScenarioDefaultsSection";
import {
  buildModelOptions,
  formatModelWithProvider,
  getModelDisplayName,
  getModelProviderDisplayName,
} from "../../../src/renderer/components/settings/ai-workbench/helpers";
import {
  getRouteCandidateModels,
  resolveRouteModel,
} from "../../../src/renderer/services/ai-defaults";
import type {
  AIModelConfig,
  AIProviderConfig,
} from "../../../src/renderer/stores/settings.store";

const PROVIDERS: AIProviderConfig[] = [
  {
    id: "p_yunwu",
    name: "云雾API",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "sk-yunwu",
    apiUrl: "https://api3.wlai.vip/v1",
  },
  {
    id: "p_gorouter",
    name: "GoRouter",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "sk-gorouter",
    apiUrl: "https://api.gorouter.app/v1",
  },
];

const MODELS: AIModelConfig[] = [
  {
    id: "m_yunwu_img",
    providerId: "p_yunwu",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "sk-yunwu",
    apiUrl: "https://api3.wlai.vip/v1",
    model: "gpt-image-2",
    capabilities: { imageGeneration: true },
  },
  {
    id: "m_gorouter_img",
    providerId: "p_gorouter",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "sk-gorouter",
    apiUrl: "https://api.gorouter.app/v1",
    model: "gpt-image-2",
    capabilities: { imageGeneration: true },
  },
  {
    id: "m_yunwu_main",
    providerId: "p_yunwu",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "sk-yunwu",
    apiUrl: "https://api3.wlai.vip/v1",
    model: "grok-4.6",
    capabilities: { chat: true },
  },
];

describe("模型路由多供应商同名模型区分", () => {
  beforeEach(async () => {
    await i18nReady;
    await changeLanguage("zh");
  });

  describe("helpers 纯函数", () => {
    it("formatModelWithProvider 正确拼接模型与供应商", () => {
      expect(formatModelWithProvider("gpt-image-2", "云雾API")).toBe(
        "gpt-image-2 (云雾API)",
      );
      expect(formatModelWithProvider("gpt-image-2", "GoRouter")).toBe(
        "gpt-image-2 (GoRouter)",
      );
      // 已包含供应商名时不重复拼接
      expect(formatModelWithProvider("云雾-gpt-4", "云雾")).toBe("云雾-gpt-4");
      // 未知或无供应商名时保持原样
      expect(formatModelWithProvider("gpt-4", undefined)).toBe("gpt-4");
      expect(formatModelWithProvider("gpt-4", "unknown")).toBe("gpt-4");
    });

    it("getModelProviderDisplayName 正确解析模型所属供应商名称", () => {
      expect(getModelProviderDisplayName(MODELS[0], PROVIDERS)).toBe("云雾API");
      expect(getModelProviderDisplayName(MODELS[1], PROVIDERS)).toBe("GoRouter");

      // 未匹配到 provider 时回退到预设标签
      const orphanModel: AIModelConfig = {
        id: "m_orphan",
        provider: "openai",
        apiProtocol: "openai",
        apiKey: "sk-test",
        apiUrl: "https://api.openai.com/v1",
        model: "gpt-4o",
      };
      expect(getModelProviderDisplayName(orphanModel, [])).toBe("OpenAI");
    });

    it("getModelDisplayName 支持传入 providers 显示供应商区分", () => {
      expect(getModelDisplayName(MODELS[0], "未配置", PROVIDERS)).toBe(
        "gpt-image-2 (云雾API)",
      );
      expect(getModelDisplayName(MODELS[1], "未配置", PROVIDERS)).toBe(
        "gpt-image-2 (GoRouter)",
      );
      expect(getModelDisplayName(null, "未配置", PROVIDERS)).toBe("未配置");
    });

    it("buildModelOptions 生成带 group、triggerLabel 和 labelText 的选项", () => {
      const options = buildModelOptions(
        [MODELS[0], MODELS[1]],
        PROVIDERS,
      );

      expect(options).toEqual([
        {
          value: "m_yunwu_img",
          label: "gpt-image-2",
          triggerLabel: "gpt-image-2 (云雾API)",
          labelText: "gpt-image-2 (云雾API)",
          group: "云雾API",
        },
        {
          value: "m_gorouter_img",
          label: "gpt-image-2",
          triggerLabel: "gpt-image-2 (GoRouter)",
          labelText: "gpt-image-2 (GoRouter)",
          group: "GoRouter",
        },
      ]);
    });
  });

  describe("ScenarioDefaultsSection 界面交互", () => {
    it("选中特定供应商模型时，触发器显示带有供应商标识的模型名", () => {
      const onRouteChange = vi.fn();

      render(
        <ScenarioDefaultsSection
          aiModels={MODELS}
          aiProviders={PROVIDERS}
          modelRouteDefaults={{ imageGen: "m_gorouter_img" }}
          onRouteChange={onRouteChange}
        />,
      );

      // 正文配图行选中的是 GoRouter 的 gpt-image-2
      const imageGenButton = screen.getByRole("button", { name: "正文配图" });
      expect(within(imageGenButton).getByText("gpt-image-2 (GoRouter)")).toBeInTheDocument();
    });

    it("打开下拉菜单后，同名模型按供应商分组展示", async () => {
      const user = userEvent.setup();
      const onRouteChange = vi.fn();

      render(
        <ScenarioDefaultsSection
          aiModels={MODELS}
          aiProviders={PROVIDERS}
          modelRouteDefaults={{ imageGen: "m_gorouter_img" }}
          onRouteChange={onRouteChange}
        />,
      );

      const imageGenButton = screen.getByRole("button", { name: "正文配图" });
      await user.click(imageGenButton);

      // 下拉菜单中应有供应商分组标题
      expect(screen.getByText("云雾API")).toBeInTheDocument();
      expect(screen.getByText("GoRouter")).toBeInTheDocument();

      // 点击云雾API下的 gpt-image-2
      const allGptImageOptions = screen.getAllByRole("option", {
        name: /gpt-image-2/,
      });
      expect(allGptImageOptions).toHaveLength(2);

      await user.click(allGptImageOptions[0]);
      expect(onRouteChange).toHaveBeenCalledWith("imageGen", "m_yunwu_img");
    });
  });

  describe("供应商临时禁用对模型路由的影响", () => {
    it("供应商被禁用时，旗下模型从候选列表中过滤", () => {
      const disabledProviders: AIProviderConfig[] = [
        { ...PROVIDERS[0], enabled: false }, // 禁用云雾API
        PROVIDERS[1], // GoRouter 仍启用
      ];

      const candidates = getRouteCandidateModels(
        MODELS,
        "imageGen",
        disabledProviders,
      );
      // 云雾的 gpt-image-2 被过滤，只剩 GoRouter 的 gpt-image-2
      expect(candidates).toHaveLength(1);
      expect(candidates[0].id).toBe("m_gorouter_img");
    });

    it("供应商被禁用时，已保存的路由默认值自动跳过该供应商的模型", () => {
      const disabledProviders: AIProviderConfig[] = [
        { ...PROVIDERS[0], enabled: false }, // 禁用云雾API
        PROVIDERS[1],
      ];

      // 原本路由配置了云雾的模型 m_yunwu_img
      const resolved = resolveRouteModel(
        MODELS,
        { imageGen: "m_yunwu_img" },
        "imageGen",
        disabledProviders,
      );

      // 云雾被禁用后，resolveRouteModel 找不到它作为候选，imageGen 无全局默认故返回 null
      expect(resolved).toBeNull();
    });

    it("UI 渲染中，被禁用供应商的模型不再出现在下拉选项中", async () => {
      const user = userEvent.setup();
      const disabledProviders: AIProviderConfig[] = [
        { ...PROVIDERS[0], enabled: false }, // 禁用云雾API
        PROVIDERS[1],
      ];

      render(
        <ScenarioDefaultsSection
          aiModels={MODELS}
          aiProviders={disabledProviders}
          modelRouteDefaults={{ imageGen: "m_gorouter_img" }}
          onRouteChange={vi.fn()}
        />,
      );

      const imageGenButton = screen.getByRole("button", { name: "正文配图" });
      await user.click(imageGenButton);

      // 下拉菜单中只应有 GoRouter，不应有云雾API
      expect(screen.queryByText("云雾API")).not.toBeInTheDocument();
      expect(screen.getByText("GoRouter")).toBeInTheDocument();

      const allGptImageOptions = screen.getAllByRole("option", {
        name: /gpt-image-2/,
      });
      expect(allGptImageOptions).toHaveLength(1);
    });
  });
});
