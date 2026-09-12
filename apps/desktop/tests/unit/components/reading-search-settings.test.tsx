import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { ReadingSearchSettings } from "../../../src/renderer/components/themed-reading/ReadingSearchSettings";

const searchConfig = vi.fn();
const initial = { configured: true, persistent: true, provider: "tavily", configuredProviders: ["tavily", "anysearch"] };
beforeAll(async () => { await i18nReady; await changeLanguage("zh"); });
beforeEach(() => {
  searchConfig.mockReset().mockResolvedValue({ success: true, search: initial });
  installWindowMocks({ api: { themedReading: { searchConfig } } });
});
describe("联网搜索设置", () => {
  it("原生搜索无需额外密钥，使用主文本模型并通过测试后保存", async () => {
    const native = { ...initial, configuredProviders: ["tavily", "anysearch", "native"], nativeModel: "gpt-6-astra" };
    searchConfig.mockResolvedValueOnce({ success: true, search: native });
    render(<ToastProvider><ReadingSearchSettings /></ToastProvider>);
    await screen.findByLabelText("Tavily API Key");
    fireEvent.click(screen.getByRole("button", { name: "搜索服务" }));
    fireEvent.click(screen.getByRole("option", { name: "模型服务原生搜索" }));
    expect(screen.getByText("使用主文本模型：gpt-6-astra")).toBeInTheDocument();
    expect(screen.queryByLabelText(/API Key/)).toBeNull();
    expect(screen.queryByRole("button", { name: "清除密钥" })).toBeNull();
    searchConfig.mockResolvedValueOnce({ success: true, search: { ...native, provider: "native" } });
    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
    await waitFor(() => expect(searchConfig).toHaveBeenLastCalledWith({ provider: "native", apiKey: undefined, test: true }));
    expect(await screen.findByText(/模型服务原生搜索 搜索连接测试通过/)).toBeInTheDocument();
  });
  it("主文本模型未配置时说明原因，不能发起原生搜索测试", async () => {
    searchConfig.mockResolvedValueOnce({ success: true, search: { ...initial, provider: "native", configured: false, nativeUnavailableReason: "请先在模型服务中配置主文本模型" } });
    render(<ToastProvider><ReadingSearchSettings /></ToastProvider>);
    expect(await screen.findByText("请先在模型服务中配置主文本模型")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "测试连接" })).toBeDisabled();
  });
  it("默认关闭，立即保存开关且不提交正在编辑的密钥；失败保留原状态", async () => {
    render(<ToastProvider><ReadingSearchSettings /></ToastProvider>);
    const toggle = await screen.findByRole("switch", { name: "生成阅读页时默认联网搜索" });
    expect(toggle).not.toBeChecked();
    fireEvent.change(screen.getByLabelText("Tavily API Key"), { target: { value: "unsaved-secret" } });
    searchConfig.mockResolvedValueOnce({ success: true, search: { ...initial, defaultEnabled: true } });
    fireEvent.click(toggle);
    await waitFor(() => expect(toggle).toBeChecked());
    expect(searchConfig).toHaveBeenLastCalledWith({ defaultEnabled: true });
    expect(screen.getByLabelText("Tavily API Key")).toHaveValue("unsaved-secret");
    searchConfig.mockResolvedValueOnce({ success: false, error: "写入失败" });
    fireEvent.click(toggle);
    expect(await screen.findByRole("alert")).toHaveTextContent("写入失败");
    expect(toggle).toBeChecked();
  });
  it("切换已配置服务无需重复输入密钥，输入不会串到另一个服务", async () => {
    render(<ToastProvider><ReadingSearchSettings /></ToastProvider>);
    fireEvent.change(await screen.findByLabelText("Tavily API Key"), { target: { value: "new-secret" } });
    fireEvent.click(screen.getByRole("button", { name: "搜索服务" }));
    fireEvent.click(screen.getByRole("option", { name: "AnySearch" }));
    expect(screen.getByLabelText("AnySearch API Key")).toHaveValue("");
    expect(screen.getByRole("button", { name: "保存", exact: true })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "保存", exact: true }));
    await waitFor(() => expect(searchConfig).toHaveBeenLastCalledWith({ provider: "anysearch", apiKey: undefined, test: false }));
  });
  it("读取失败显示错误和重试，不冒充未配置", async () => {
    searchConfig.mockResolvedValueOnce({ success: false, error: "安全存储读取失败" });
    render(<ToastProvider><ReadingSearchSettings /></ToastProvider>);
    expect(await screen.findByRole("alert")).toHaveTextContent("安全存储读取失败");
    expect(screen.queryByText(/尚未配置/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重试读取" }));
    expect(await screen.findByLabelText("Tavily API Key")).toBeInTheDocument();
  });
  it("测试过程中有进度，失败后保留输入可重试", async () => {
    render(<ToastProvider><ReadingSearchSettings /></ToastProvider>);
    fireEvent.change(await screen.findByLabelText("Tavily API Key"), { target: { value: "replacement-secret" } });
    let finish: (value: unknown) => void;
    searchConfig.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
    expect(await screen.findByRole("status")).toHaveTextContent("正在测试搜索连接");
    finish!({ success: false, error: "搜索额度已用尽" });
    await waitFor(() => expect(screen.getByLabelText("Tavily API Key")).toBeEnabled());
    expect(screen.getByLabelText("Tavily API Key")).toHaveValue("replacement-secret");
    expect(screen.getByRole("alert")).toHaveTextContent("搜索额度已用尽");
  });
});
