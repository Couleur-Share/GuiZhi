package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"guizhi-mygo-shell/native/services"
)

// settingsAdvancedFixtureJSON 是设置页三个外围分区（采集与转写、MCP 设置、AI 模型服务）对照场景的数据夹具：
// 与 React 采集器（scripts/parity/lib/react-settings-advanced.mjs）读的是同一个文件，两侧因此处在同一组合成数据里。
// 字段与 IPC 回复一致（services.ToolStatus、PlatformStatus、MCPConfig、AIProvider 等的 JSON 标签），
// React 一侧页内替换对应频道（ytdlp:status、mcp:config、ai:usageSummary……）时原样返回。
//
// 口令策略同 SettingsCoreProxy：端点里的 apiKey 是合成的占位串（parity-fixture-key-…），只在「模型路由」面板之外的
// 端点卡里以密码框出现；场景不打开端点卡，也不在表单里输入口令，所以两侧的可访问树里都没有口令值。
//
//go:embed testdata/settings-advanced-fixture.json
var settingsAdvancedFixtureJSON []byte

// SettingsAdvancedFixture 是夹具的内容。
type SettingsAdvancedFixture struct {
	// Platform 是运行平台（Node 写法）：说话人分离开关据此可用与否。
	Platform string `json:"platform"`
	// Tools 是两个二进制引擎的探测结果（ytdlp:status、ffmpeg:status），键是 ytdlp 与 ffmpeg。
	Tools map[string]services.ToolStatus `json:"tools"`
	// Funasr 是本地转写引擎的状态（funasr:status）。
	Funasr services.FunasrStatus `json:"funasr"`
	// Accounts 是各平台账号的登录状态（platformCapture:status）。
	Accounts []services.PlatformStatus `json:"accounts"`
	// MCP 是 MCP server 的启动信息与可访问范围（mcp:config、mcp:getScope）。
	MCP struct {
		Config services.MCPConfig `json:"config"`
		Scope  services.MCPScope  `json:"scope"`
	} `json:"mcp"`
	// AI 是模型服务的端点、模型、路由与最近 30 天用量（settings:get 的 AI 部分、ai:usageSummary）。
	AI struct {
		Providers []services.AIProvider   `json:"providers"`
		Models    []services.AIModel      `json:"models"`
		Routes    map[string]string       `json:"routes"`
		Usage     services.AIUsageSummary `json:"usage"`
	} `json:"ai"`
	// ReadingSearch 是「联网搜索」块读到的搜索配置（themedReading:searchConfig 的只读回复）；只有 React 一侧用得上，原样保留。
	ReadingSearch json.RawMessage `json:"readingSearch"`
}

// AIConfig 是夹具里的整份 AI 配置，形状同 services.AISettings.Load 的返回。
func (f SettingsAdvancedFixture) AIConfig() services.AIConfig {
	return services.AIConfig{Providers: f.AI.Providers, Models: f.AI.Models, Routes: f.AI.Routes}.Clone()
}

// LoadSettingsAdvancedFixture 解码夹具。
func LoadSettingsAdvancedFixture() (SettingsAdvancedFixture, error) {
	var f SettingsAdvancedFixture
	if err := json.Unmarshal(settingsAdvancedFixtureJSON, &f); err != nil {
		return SettingsAdvancedFixture{}, fmt.Errorf("解码设置外围分区夹具失败：%w", err)
	}
	return f, nil
}
