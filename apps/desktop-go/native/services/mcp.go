package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// 设置页「MCP 设置」分区依赖的系统能力：MCP server 的启动信息、可访问范围、一键写入 Cursor。
//
// 对应主进程的 mcp:config / mcp:getScope / mcp:setScope / mcp:install 频道。归知的 MCP server 是本程序自己的 --mcp 模式，
// 随应用打包、跟着应用升级：没有服务开关，也没有令牌（只提供检索与读取两个只读工具，范围由 MCPScope 限定）。
// 客户端接入片段（Cursor 的 JSON、Codex 的 TOML 与命令行）是纯函数，在本包里生成（见 BuildCursorConfig 等）。

// MCPServerName 是客户端里显示的服务器名（Codex 要求只含字母、数字、连字符与下划线）。
const MCPServerName = "guizhi"

// MCP 可访问范围的模式。
const (
	MCPScopeAll      = "all"
	MCPScopeSelected = "selected"
)

// MCPClient 是支持的 AI 客户端。
type MCPClient string

const (
	MCPClientCursor MCPClient = "cursor"
	MCPClientCodex  MCPClient = "codex"
)

// MCPConfig 是 MCP server 的启动信息（mcp:config）。
//
// Env 是启动时带给 server 的环境变量：当前实现恒为空，但接入片段会把它原样写进客户端配置，
// 所以按机密对待——String / GoString 只输出键名，不输出值，避免误进日志与错误详情。
type MCPConfig struct {
	// Available 为假表示没有找到 MCP server 组件（安装包不完整）。
	Available  bool              `json:"available"`
	ServerPath string            `json:"serverPath"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
}

// String 给日志与 %v 用：环境变量只留键名，值一律脱敏。
func (c MCPConfig) String() string {
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k+"=***")
	}
	sort.Strings(keys)
	return fmt.Sprintf("{available=%t serverPath=%s command=%s args=%v env=[%s]}",
		c.Available, c.ServerPath, c.Command, c.Args, strings.Join(keys, " "))
}

// GoString 让 %#v 同样不泄露环境变量的值。
func (c MCPConfig) GoString() string { return "services.MCPConfig" + c.String() }

// MCPScope 是外部 AI 工具能看见哪些知识库（mcp.json）。
type MCPScope struct {
	// Mode 是 all 或 selected；selected 时只有 AllowedCollectionIDs 里的合集（与 AllowUncategorized）可见。
	Mode string `json:"mode"`
	// AllowedCollectionIDs 存合集 ID 而不是名字：改名不该让范围失效。
	AllowedCollectionIDs []string `json:"allowedCollectionIds"`
	// AllowUncategorized 决定没归入任何合集的条目算不算在内。
	AllowUncategorized bool `json:"allowUncategorized"`
}

// DefaultMCPScope 是默认范围：全部可见。
func DefaultMCPScope() MCPScope {
	return MCPScope{Mode: MCPScopeAll, AllowedCollectionIDs: []string{}, AllowUncategorized: true}
}

// Clone 返回深拷贝（ID 列表是切片）。
func (s MCPScope) Clone() MCPScope {
	s.AllowedCollectionIDs = append([]string{}, s.AllowedCollectionIDs...)
	return s
}

// IsEmpty 报告范围是否「一条都不给看」：selected 且没勾任何合集、也不含未归合集。界面据此提醒，否则用户只会看到 MCP 突然搜不到东西。
func (s MCPScope) IsEmpty() bool {
	return s.Mode == MCPScopeSelected && len(s.AllowedCollectionIDs) == 0 && !s.AllowUncategorized
}

// HasCollection 报告合集是否在放行列表里。
func (s MCPScope) HasCollection(id string) bool {
	for _, v := range s.AllowedCollectionIDs {
		if v == id {
			return true
		}
	}
	return false
}

// ToggleCollection 返回勾选 / 取消勾选一个合集后的范围（不改原值）。
func (s MCPScope) ToggleCollection(id string) MCPScope {
	next := s.Clone()
	if s.HasCollection(id) {
		kept := next.AllowedCollectionIDs[:0]
		for _, v := range next.AllowedCollectionIDs {
			if v != id {
				kept = append(kept, v)
			}
		}
		next.AllowedCollectionIDs = kept
		return next
	}
	next.AllowedCollectionIDs = append(next.AllowedCollectionIDs, id)
	return next
}

// MCPInstallResult 是一键写入的结果。
type MCPInstallResult struct {
	// FilePath 是被写入的客户端配置文件。
	FilePath string
	// Replaced 为真表示文件里已有归知的条目，这次是更新而不是新增。
	Replaced bool
}

// MCP 是 MCP 接入设置的系统动作。失败一律返回带原因的错误。
type MCP interface {
	// Config 读取 MCP server 的启动信息。
	Config(ctx context.Context) (MCPConfig, error)
	// Scope 读取可访问范围；文件不存在或损坏时是「全部可见」。
	Scope(ctx context.Context) (MCPScope, error)
	// SetScope 保存可访问范围并返回规整后的值；MCP server 每次工具调用都会重读，所以立即生效。
	SetScope(ctx context.Context, scope MCPScope) (MCPScope, error)
	// Install 把归知合并进客户端的配置文件（已有的其它服务器与注释保留，覆盖前留一份备份）。目前只支持 Cursor。
	Install(ctx context.Context, client MCPClient) (MCPInstallResult, error)
}

// mcpLaunch 是接入片段共用的启动描述：写进 JSON 时键序固定为 command、args、env。
type mcpLaunch struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func newMCPLaunch(c MCPConfig) mcpLaunch {
	args := c.Args
	if args == nil {
		args = []string{}
	}
	return mcpLaunch{Command: c.Command, Args: args, Env: c.Env}
}

// jsonString 与 JSON.stringify 一致：不转义 < > &。
func jsonString(v any, indent bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	_ = enc.Encode(v) // 只含字符串、切片与 map，不会失败
	return strings.TrimSuffix(buf.String(), "\n")
}

// shellArg 在命令行参数含空白或引号时加引号（Windows 的 C:\Program Files\... 必然需要）。
func shellArg(v string) string {
	if strings.IndexFunc(v, func(r rune) bool { return unicode.IsSpace(r) || r == '"' }) >= 0 {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

// tomlString 优先用单引号字面量（不处理转义，Windows 路径里的反斜杠可以原样写）；值含单引号或换行时回退到基本字符串。
func tomlString(v string) string {
	if !strings.ContainsAny(v, "'\n") {
		return "'" + v + "'"
	}
	return jsonString(v, false)
}

func tomlArray(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = tomlString(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// BuildCursorConfig 是 Cursor 的配置片段（mcpServers，JSON，缩进 2 空格、结尾换行）。
// 环境变量的键按字母序排列（Go 的 map 没有插入顺序；当前恒为空）。
func BuildCursorConfig(c MCPConfig) string {
	doc := struct {
		MCPServers map[string]mcpLaunch `json:"mcpServers"`
	}{MCPServers: map[string]mcpLaunch{MCPServerName: newMCPLaunch(c)}}
	return jsonString(doc, true) + "\n"
}

// BuildCodexToml 是 Codex 的 config.toml 片段：键是 snake_case 的 mcp_servers，写成 mcpServers 会被整段忽略且不报错。
func BuildCodexToml(c MCPConfig) string {
	lines := []string{
		"[mcp_servers." + MCPServerName + "]",
		"command = " + tomlString(c.Command),
		"args = " + tomlArray(newMCPLaunch(c).Args),
	}
	if len(c.Env) > 0 {
		lines = append(lines, "", "[mcp_servers."+MCPServerName+".env]")
		for _, k := range sortedEnvKeys(c.Env) {
			lines = append(lines, k+" = "+tomlString(c.Env[k]))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// BuildCodexAddCommand 是 codex mcp add 的命令行：--env 排在服务器名之后、-- 之前（claude mcp add 恰好相反，抄串了不报错）。
func BuildCodexAddCommand(c MCPConfig) string {
	parts := []string{"codex", "mcp", "add", MCPServerName}
	for _, k := range sortedEnvKeys(c.Env) {
		parts = append(parts, "--env", k+"="+c.Env[k])
	}
	parts = append(parts, "--", shellArg(c.Command))
	for _, a := range newMCPLaunch(c).Args {
		parts = append(parts, shellArg(a))
	}
	return strings.Join(parts, " ")
}
