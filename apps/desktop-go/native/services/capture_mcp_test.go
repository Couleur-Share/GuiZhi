package services

import (
	"fmt"
	"strings"
	"testing"
)

// 引擎版本统一按日期展示：yt-dlp 的发布日、ffmpeg 每日构建尾部的日期；拿不到日期的退回主版本号或原串（与渲染层 engine-version.ts 一致）。
func TestFormatEngineVersion(t *testing.T) {
	for in, want := range map[string]string{
		"2026.07.04":                        "2026-07-04",
		"  2026.07.04 ":                     "2026-07-04",
		"N-125753-g6095372a70-20260724":     "2026-07-24",
		"20260724":                          "2026-07-24",
		"8.1-essentials_build-www.gyan.dev": "8.1",
		"nightly-abc":                       "nightly-abc",
		"":                                  "",
		"1234567890":                        "1234567890",
		"2026.7.4":                          "2026.7.4",
	} {
		if got := FormatEngineVersion(in); got != want {
			t.Errorf("FormatEngineVersion(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func windowsConfig() MCPConfig {
	return MCPConfig{Available: true, Command: `C:\Program Files\GuiZhi\GuiZhi.exe`, Args: []string{"--mcp"}, Env: map[string]string{}}
}

// Cursor 片段：mcpServers 的 JSON，缩进 2 空格、结尾换行；反斜杠按 JSON 转义；没有环境变量时不写 env 键。
func TestBuildCursorConfig(t *testing.T) {
	want := "{\n  \"mcpServers\": {\n    \"guizhi\": {\n      \"command\": \"C:\\\\Program Files\\\\GuiZhi\\\\GuiZhi.exe\",\n      \"args\": [\n        \"--mcp\"\n      ]\n    }\n  }\n}\n"
	if got := BuildCursorConfig(windowsConfig()); got != want {
		t.Errorf("Cursor 片段：\n%q\n期望\n%q", got, want)
	}
	cfg := windowsConfig()
	cfg.Args, cfg.Env = nil, map[string]string{"B": "2", "A": "<1>&"}
	got := BuildCursorConfig(cfg)
	if !strings.Contains(got, "\"args\": []") {
		t.Errorf("空参数应是 []，不是 null：%s", got)
	}
	if strings.Index(got, "\"A\"") > strings.Index(got, "\"B\"") || !strings.Contains(got, "\"A\": \"<1>&\"") {
		t.Errorf("环境变量按键名排序，且不转义 < > &：%s", got)
	}
}

// Codex 的 TOML：键是 mcp_servers；单引号字面量让 Windows 路径原样写；含单引号或换行的值回退到双引号基本字符串。
func TestBuildCodexToml(t *testing.T) {
	want := "[mcp_servers.guizhi]\ncommand = 'C:\\Program Files\\GuiZhi\\GuiZhi.exe'\nargs = ['--mcp']\n"
	if got := BuildCodexToml(windowsConfig()); got != want {
		t.Errorf("TOML：\n%q\n期望\n%q", got, want)
	}
	cfg := windowsConfig()
	cfg.Command = "it's"
	cfg.Env = map[string]string{"K": "a\nb", "J": "plain"}
	got := BuildCodexToml(cfg)
	for _, frag := range []string{"command = \"it's\"", "\n[mcp_servers.guizhi.env]\n", "J = 'plain'", "K = \"a\\nb\""} {
		if !strings.Contains(got, frag) {
			t.Errorf("TOML 缺少 %q：\n%s", frag, got)
		}
	}
	if strings.Index(got, "J = ") > strings.Index(got, "K = ") {
		t.Errorf("环境变量按键名排序：\n%s", got)
	}
}

// codex mcp add：--env 在服务器名之后、-- 之前；含空白或引号的参数加引号并转义。
func TestBuildCodexAddCommand(t *testing.T) {
	want := `codex mcp add guizhi -- "C:\Program Files\GuiZhi\GuiZhi.exe" --mcp`
	if got := BuildCodexAddCommand(windowsConfig()); got != want {
		t.Errorf("命令行：%q 期望 %q", got, want)
	}
	cfg := MCPConfig{Command: "guizhi", Args: []string{`say "hi"`, "plain"}, Env: map[string]string{"B": "2", "A": "1"}}
	want = `codex mcp add guizhi --env A=1 --env B=2 -- guizhi "say \"hi\"" plain`
	if got := BuildCodexAddCommand(cfg); got != want {
		t.Errorf("带环境变量与引号的命令行：%q 期望 %q", got, want)
	}
}

// MCPConfig 的环境变量是机密：%v、%+v、%#v、%s 都只出键名。
func TestMCPConfigRedactsEnv(t *testing.T) {
	cfg := MCPConfig{Available: true, Command: "x", Args: []string{"--mcp"}, Env: map[string]string{"API_TOKEN": "sk-very-secret"}}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		out := fmt.Sprintf(verb, cfg)
		if strings.Contains(out, "sk-very-secret") || !strings.Contains(out, "API_TOKEN=***") {
			t.Errorf("%s 输出应脱敏且保留键名：%s", verb, out)
		}
	}
	if out := fmt.Sprint(&cfg); strings.Contains(out, "sk-very-secret") {
		t.Errorf("指针格式化也不能泄露：%s", out)
	}
}

// 范围的勾选不改原值；去掉一项保持其余顺序；全空才算「一条都不给看」。
func TestMCPScopeToggle(t *testing.T) {
	base := MCPScope{Mode: MCPScopeSelected, AllowedCollectionIDs: []string{"a", "b"}, AllowUncategorized: false}
	next := base.ToggleCollection("c")
	if len(base.AllowedCollectionIDs) != 2 || len(next.AllowedCollectionIDs) != 3 || !next.HasCollection("c") {
		t.Fatalf("勾选：原 %v 新 %v", base.AllowedCollectionIDs, next.AllowedCollectionIDs)
	}
	off := next.ToggleCollection("a")
	if strings.Join(off.AllowedCollectionIDs, ",") != "b,c" || strings.Join(next.AllowedCollectionIDs, ",") != "a,b,c" {
		t.Fatalf("取消勾选：新 %v 原 %v", off.AllowedCollectionIDs, next.AllowedCollectionIDs)
	}
	if base.IsEmpty() || !(MCPScope{Mode: MCPScopeSelected}).IsEmpty() ||
		(MCPScope{Mode: MCPScopeSelected, AllowUncategorized: true}).IsEmpty() || (MCPScope{Mode: MCPScopeAll}).IsEmpty() {
		t.Error("IsEmpty 只在 selected 且没勾合集、也不含未归合集时为真")
	}
	if d := DefaultMCPScope(); d.Mode != MCPScopeAll || !d.AllowUncategorized || d.AllowedCollectionIDs == nil {
		t.Errorf("默认范围：%+v", d)
	}
}
