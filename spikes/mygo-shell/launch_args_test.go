package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestStripLaunchArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"无参数", nil, []string{}},
		{"安装器拉起", []string{"--updated"}, []string{}},
		{"开机启动项", []string{"--hidden"}, []string{}},
		{"更新器透传", []string{"--updated", "--force-run"}, []string{}},
		{"与真实开关并存，保持顺序", []string{"--hidden", "-userdata", "D:\\ud", "--updated", "-wait", "3s"}, []string{"-userdata", "D:\\ud", "-wait", "3s"}},
		{"位置参数不动", []string{"-backupcli", "export", "--hidden", "x"}, []string{"-backupcli", "export", "x"}},
		{"近似名字不误删", []string{"--hidden2", "-hidden"}, []string{"--hidden2", "-hidden"}},
	}
	for _, c := range cases {
		in := append([]string(nil), c.in...)
		got := stripLaunchArgs(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
		if !reflect.DeepEqual(in, append([]string(nil), c.in...)) && len(c.in) > 0 {
			t.Errorf("%s: 不应修改入参", c.name)
		}
	}
}

// 回归：不摘参数时 flag 对 --updated / --hidden 报错（真实现象是进程 exit 2、窗口都没出现）；摘掉后正常解析。
func TestLaunchArgsDoNotBreakFlagParse(t *testing.T) {
	newSet := func() (*flag.FlagSet, *string) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(discardWriter{})
		u := fs.String("userdata", "", "")
		return fs, u
	}
	raw := []string{"--updated", "-userdata", "x", "--hidden"}
	fs, _ := newSet()
	if err := fs.Parse(raw); err == nil {
		t.Fatal("预期未摘参数时 flag 解析失败（这是缺陷的根因）")
	}
	fs, u := newSet()
	if err := fs.Parse(stripLaunchArgs(raw)); err != nil {
		t.Fatalf("摘参数后应能解析: %v", err)
	}
	if *u != "x" {
		t.Fatalf("userdata=%q", *u)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
