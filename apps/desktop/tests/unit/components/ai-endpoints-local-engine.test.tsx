import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  FUNASR_BASE_URL,
  FUNASR_MODEL_ID,
  FUNASR_PROVIDER_ID,
} from "@guizhi/shared/constants";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { EndpointsSection } from "../../../src/renderer/components/settings/ai-workbench/EndpointsSection";
import type { EndpointGroup } from "../../../src/renderer/components/settings/ai-workbench/types";

/**
 * 内置本地转写引擎在「模型服务」里的只读呈现。
 *
 * 这两条记录（服务商 + 模型）只在「设置 → 采集」一键安装时写入、卸载时移除，
 * 没有自愈路径：在这个页面上删掉模型或改掉地址，磁盘上 3GB 运行时还在、
 * 转写却彻底失效，界面上也看不出发生过什么。
 */
const BUILTIN_GROUP: EndpointGroup = {
  key: `provider:${FUNASR_PROVIDER_ID}`,
  providerConfigId: FUNASR_PROVIDER_ID,
  name: "本地转写引擎",
  provider: "custom",
  apiProtocol: "openai",
  apiKey: "local",
  apiUrl: FUNASR_BASE_URL,
  models: [
    {
      id: FUNASR_MODEL_ID,
      name: "SenseVoiceSmall（本地）",
      providerId: FUNASR_PROVIDER_ID,
      provider: "custom",
      apiProtocol: "openai",
      apiKey: "local",
      apiUrl: FUNASR_BASE_URL,
      model: "sensevoice",
      capabilities: { chat: false, audioTranscription: true },
    },
  ],
};

const CLOUD_GROUP: EndpointGroup = {
  key: "provider:p_cloud",
  providerConfigId: "p_cloud",
  name: "云雾API",
  provider: "custom",
  apiProtocol: "openai",
  apiKey: "sk-0123456789abcdef",
  apiUrl: "https://api3.wlai.vip/v1",
  models: [
    {
      id: "m_cloud",
      name: "gpt-4o",
      providerId: "p_cloud",
      provider: "custom",
      apiProtocol: "openai",
      apiKey: "sk-0123456789abcdef",
      apiUrl: "https://api3.wlai.vip/v1",
      model: "gpt-4o",
      capabilities: { chat: true },
    },
  ],
};

const noop = () => {};

function renderSection(
  groups: EndpointGroup[],
  overrides?: { onManageLocalEngine?: () => void },
) {
  return render(
    <EndpointsSection
      routingContent={null}
      endpointGroups={groups}
      endpointStatuses={{}}
      testingEndpointKey={null}
      testingModelId={null}
      modelScenarioBadges={new Map()}
      onTestEndpoint={noop}
      onEditEndpoint={noop}
      onDeleteEndpoint={noop}
      onUpdateEndpointCredentials={noop}
      onAddProvider={noop}
      onAddModel={noop}
      onFetchModels={noop}
      onSetDefaultModel={noop}
      onTestModel={noop}
      onEditModel={noop}
      onDeleteModel={noop}
      onManageLocalEngine={overrides?.onManageLocalEngine ?? noop}
    />,
  );
}

function modelRow(modelId: string) {
  return within(screen.getByTestId(`ai-model-row-${modelId}`));
}

beforeEach(async () => {
  await i18nReady;
  await changeLanguage("zh");
});

describe("模型服务里的内置本地转写引擎", () => {
  /**
   * 保护此前只加在服务商卡片的「删除」上，模型行漏掉了：删掉
   * SenseVoiceSmall 的后果与删掉整个服务商完全一样，且更容易误点。
   */
  it("内置模型的删除与编辑都锁上，卸载入口指向采集设置", () => {
    renderSection([BUILTIN_GROUP]);

    const row = modelRow(FUNASR_MODEL_ID);
    expect(row.getByRole("button", { name: "删除" })).toBeDisabled();
    expect(row.getByRole("button", { name: "编辑" })).toBeDisabled();
    expect(screen.getByTestId("ai-endpoint-delete")).toBeDisabled();
    expect(screen.getByTestId("ai-endpoint-edit")).toBeDisabled();
  });

  /**
   * 用户的原话是「根本不知道如何去填写这个 API 地址和 API Key」——
   * 值是安装程序填的（Key 就是占位串 local，本地服务不校验），
   * 摆成输入框等于在说这里缺东西。
   */
  it("地址只读、不出现 Key 输入框", () => {
    renderSection([BUILTIN_GROUP]);

    expect(screen.queryByLabelText("端点 API 地址")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("端点 API Key")).not.toBeInTheDocument();
    expect(screen.getByText(FUNASR_BASE_URL)).toBeInTheDocument();
    expect(screen.getByText("内置")).toBeInTheDocument();
  });

  it("「管理引擎」把用户送去采集设置", async () => {
    const onManageLocalEngine = vi.fn();
    const user = userEvent.setup();
    renderSection([BUILTIN_GROUP], { onManageLocalEngine });

    await user.click(screen.getByRole("button", { name: "管理引擎" }));
    expect(onManageLocalEngine).toHaveBeenCalledTimes(1);
  });

  /**
   * 对照组：锁定只认安装写入的那个固定 id。用户自己配的服务商一切照旧，
   * 包括那些自己在 127.0.0.1:8620 上跑服务、手工添加进来的。
   */
  it("用户自己配的服务商不受影响", () => {
    renderSection([CLOUD_GROUP]);

    expect(modelRow("m_cloud").getByRole("button", { name: "删除" })).toBeEnabled();
    expect(screen.getByTestId("ai-endpoint-delete")).toBeEnabled();
    expect(screen.getByLabelText("端点 API 地址")).toBeInTheDocument();
    expect(screen.getByLabelText("端点 API Key")).toBeInTheDocument();
    expect(screen.queryByText("内置")).not.toBeInTheDocument();
  });
});
