package main

import "testing"

// 登录项拉起的判定读原始命令行：mygo 的 init() 已把参数从 os.Args 摘掉，所以这里是纯函数，输入来自操作系统命令行。
func TestWasOpenedAtLoginFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"无参数", nil, false},
		{"普通启动", []string{"-userdata", "x"}, false},
		{"登录项拉起", []string{"--mygo-opened-at-login"}, true},
		{"与其他参数并存", []string{"-wait", "3s", "--mygo-opened-at-login", "--updated"}, true},
		{"含 argv[0]", []string{`C:\Program Files\GuiZhi\GuiZhi.exe`, "--mygo-opened-at-login"}, true},
		{"近似名字不算", []string{"--mygo-opened-at-login2", "-mygo-opened-at-login", "--hidden"}, false},
	}
	for _, c := range cases {
		if got := wasOpenedAtLoginFromArgs(c.args); got != c.want {
			t.Errorf("%s：got %v want %v", c.name, got, c.want)
		}
	}
}

// 启动即隐藏：登录项拉起且设置了「启动时最小化」才驻留托盘；手动打开始终显示；--hidden 旧登记照旧生效。
func TestVivaLaunchVisibility(t *testing.T) {
	login := []string{"C:\\GuiZhi.exe", "--mygo-opened-at-login"}
	cases := []struct {
		name           string
		args           []string
		minimize       bool
		shouldMaximize bool
		wantHidden     bool
	}{
		{"手动启动", nil, true, false, false},
		{"登录项拉起 + 设置了启动时隐藏", login, true, false, true},
		{"登录项拉起 + 没设置", login, false, false, false},
		{"旧版 --hidden 登记", []string{"--hidden"}, false, false, true},
		{"登录项拉起 + 隐藏 + 记忆为最大化：首次显示时再最大化", login, true, true, true},
	}
	for _, c := range cases {
		vis := vivaLaunchVisibility(c.args, c.minimize, c.shouldMaximize)
		if vis.StartHidden != c.wantHidden {
			t.Errorf("%s：StartHidden=%v want %v", c.name, vis.StartHidden, c.wantHidden)
		}
	}
	if vis := vivaLaunchVisibility(login, true, true); !vis.MaximizeOnFirstShow || vis.MaximizeNow {
		t.Errorf("隐藏启动应把最大化推迟到首次显示：%+v", vis)
	}
}

func TestLoginCommandLine(t *testing.T) {
	got := loginCommandLine(`C:\Program Files\GuiZhi\GuiZhi.exe`)
	want := `"C:\Program Files\GuiZhi\GuiZhi.exe" --mygo-opened-at-login`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
