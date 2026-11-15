package servicestest

import (
	"errors"
	"testing"

	"guizhi-mygo-shell/native/settings"
)

func TestSettingsFakeUpdateSemantics(t *testing.T) {
	var f Settings
	var seen []settings.Values
	cancel := f.Subscribe(func(v settings.Values) {
		seen = append(seen, v)
		// 订阅者里再读设置不能死锁
		if _, err := f.Snapshot(); err != nil {
			t.Error(err)
		}
	})

	got, err := f.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeDark; v.UI.ThemeHue = 999 })
	if err != nil || got.UI.ThemeMode != settings.ThemeDark || got.UI.ThemeHue != 360 {
		t.Fatalf("写入应归一化后生效：%+v %v", got.UI, err)
	}
	if len(seen) != 1 || f.Updates() != 1 {
		t.Fatalf("应通知一次：通知 %d 次，写入 %d 次", len(seen), f.Updates())
	}

	if _, err := f.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeDark }); err != nil || len(seen) != 1 || f.Updates() != 1 {
		t.Fatalf("没有变化不该通知也不该计一次写入：%v 通知 %d 写入 %d", err, len(seen), f.Updates())
	}

	boom := errors.New("磁盘已满")
	f.FailWith(boom)
	before := f.Values()
	if _, err := f.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeLight }); !errors.Is(err, boom) {
		t.Fatalf("应返回注入的错误：%v", err)
	}
	if !f.Values().Equal(before) || len(seen) != 1 {
		t.Fatal("失败不能改值也不能通知")
	}
	f.FailWith(nil)

	cancel()
	cancel() // 可重复取消
	if f.Subscribers() != 0 {
		t.Fatal("取消后不该还有订阅者")
	}
	if _, err := f.Update(func(v *settings.Values) { v.UI.ThemeMode = settings.ThemeLight }); err != nil || len(seen) != 1 {
		t.Fatalf("取消后不再通知：%v 通知 %d", err, len(seen))
	}
}

func TestPrefsFake(t *testing.T) {
	var p Prefs
	if _, ok := p.Get("a"); ok {
		t.Fatal("空表不该有值")
	}
	if err := p.Set("a", map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	if raw, ok := p.Get("a"); !ok || string(raw) != `{"x":1}` {
		t.Fatalf("读回不对：%s %v", raw, ok)
	}
	p.Put("bad", "{not json")
	if raw, _ := p.Get("bad"); string(raw) != "{not json" {
		t.Fatal("Put 应原样放入损坏的数据")
	}
	boom := errors.New("写入失败")
	p.FailSetWith(boom)
	if err := p.Set("b", 1); !errors.Is(err, boom) {
		t.Fatalf("应返回注入的错误：%v", err)
	}
	if _, ok := p.Get("b"); ok {
		t.Fatal("失败的写入不该留下值")
	}
	if keys := p.SetKeys(); len(keys) != 1 || keys[0] != "a" {
		t.Fatalf("只应记录成功的写入：%v", keys)
	}
}
