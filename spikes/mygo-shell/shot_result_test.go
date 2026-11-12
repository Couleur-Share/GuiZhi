package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestShotResultPath(t *testing.T) {
	cases := map[string]string{
		`C:\x\out.png`:    `C:\x\out.7.result.json`,
		`C:\a.b\out`:      `C:\a.b\out.7.result.json`, // 目录里的点不是扩展名
		`/tmp/shot.1.png`: `/tmp/shot.1.7.result.json`,
	}
	for in, want := range cases {
		if got := shotResultPath(in, 7); got != want {
			t.Errorf("shotResultPath(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestWriteShotResultRoundTrip(t *testing.T) {
	shot := filepath.Join(t.TempDir(), "o.png")
	writeShotResult(shot, map[string]any{"n": 22.0}, nil)
	data, err := os.ReadFile(shotResultPath(shot, os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Value map[string]any `json:"value"`
		Error string         `json:"error"`
	}
	if err := json.Unmarshal(data, &rec); err != nil || rec.Value["n"] != 22.0 || rec.Error != "" {
		t.Fatalf("结果不符：%s（%v）", data, err)
	}
	writeShotResult(shot, nil, errors.New("js 抛错"))
	data, _ = os.ReadFile(shotResultPath(shot, os.Getpid()))
	_ = json.Unmarshal(data, &rec)
	if rec.Error != "js 抛错" {
		t.Fatalf("错误应被记录：%s", data)
	}
}
