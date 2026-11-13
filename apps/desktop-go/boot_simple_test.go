package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func callBoot(t *testing.T, b *Bridge, channel string, args ...string) any {
	t.Helper()
	raws := make([]json.RawMessage, len(args))
	for i, a := range args {
		raws[i] = json.RawMessage(a)
	}
	v, err := b.handlers[channel](context.Background(), raws)
	if err != nil {
		t.Fatal(err)
	}
	return normalize(t, v)
}

func TestSimpleBootChannels(t *testing.T) {
	b := &Bridge{handlers: map[string]handler{}}
	backup := &rendererBackupSettings{}
	b.registerSimpleBoot(backup)

	if got := callBoot(t, b, "updater:version"); got != appVersion {
		t.Fatalf("version = %v", got)
	}

	skip := map[string]any{"success": true}
	fail := map[string]any{"success": false}
	cases := []struct {
		arg  string
		want map[string]any
	}{
		{`{"trigger":"startup","reason":"offline"}`, skip},
		{`{"trigger":"startup","reason":"nope"}`, fail},
		{`{"trigger":"nope","reason":"offline"}`, fail},
		{`{"trigger":"manual"}`, fail},
		{`null`, fail},
		{`"x"`, fail},
	}
	for _, c := range cases {
		if got := callBoot(t, b, "updater:logAutoSkip", c.arg); !reflect.DeepEqual(got, any(c.want)) {
			t.Errorf("logAutoSkip(%s) = %v", c.arg, got)
		}
	}
	// 缺参数（preload 不带 payload 调用）同样返回 success:false
	if got := callBoot(t, b, "updater:logAutoSkip"); !reflect.DeepEqual(got, any(fail)) {
		t.Errorf("logAutoSkip() = %v", got)
	}

	for _, bad := range []string{`null`, `[]`, `"s"`, `1`} {
		if got := callBoot(t, b, "backupRepository:syncRendererSettings", bad); got != false {
			t.Errorf("sync(%s) = %v", bad, got)
		}
	}
	if backup.get() != nil {
		t.Fatal("非法入参不应写入")
	}
	if got := callBoot(t, b, "backupRepository:syncRendererSettings", `{"autoBackup":true}`); got != true {
		t.Errorf("sync(object) = %v", got)
	}
	if !reflect.DeepEqual(backup.get(), map[string]any{"autoBackup": true}) {
		t.Errorf("backup settings = %v", backup.get())
	}
}
