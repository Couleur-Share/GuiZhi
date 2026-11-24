package servicestest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(t.Context())
}

// waitUntil 轮询到条件成立，超时报错；等的是另一个 goroutine 里的状态（比如登录已停在等待里）。
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// 默认登录立刻成功并把状态改成已登录；参数被记录；列表里没有的平台会补一项。
func TestPlatformAuthFakeLogin(t *testing.T) {
	ctx := t.Context()
	f := &PlatformAuth{}
	f.SetStatuses(services.PlatformStatus{Platform: "douyin", Available: true})

	st, err := f.Login(ctx, "douyin", services.PlatformLoginOptions{ForceRelogin: true})
	if err != nil || !st.LoggedIn {
		t.Fatalf("登录：%+v %v", st, err)
	}
	list, _ := f.Statuses(ctx)
	if got, ok := services.FindPlatformStatus(list, "douyin"); !ok || !got.LoggedIn {
		t.Fatalf("状态应已登录：%+v", list)
	}
	if _, err := f.Login(ctx, services.PlatformNodeseek, services.PlatformLoginOptions{VerificationURL: "https://www.nodeseek.com/post-1-1"}); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.Statuses(ctx); len(list) != 2 {
		t.Fatalf("没有的平台应补一项：%+v", list)
	}
	opts := f.LoginOptions()
	if len(opts) != 2 || !opts[0].ForceRelogin || opts[1].VerificationURL != "https://www.nodeseek.com/post-1-1" {
		t.Fatalf("登录参数：%+v", opts)
	}
	if _, ok := services.FindPlatformStatus(list, "linuxdo"); ok {
		t.Fatal("不存在的平台不应找到")
	}
}

// 注入失败：状态读取、某平台登录、取消各自可失败，失败的登录不改状态。
func TestPlatformAuthFakeFailures(t *testing.T) {
	ctx := t.Context()
	f := &PlatformAuth{}
	f.SetStatuses(services.PlatformStatus{Platform: "douyin", Available: true})
	boom := errors.New("embedded browser unavailable")

	f.FailStatuses(boom)
	if _, err := f.Statuses(ctx); !errors.Is(err, boom) {
		t.Fatalf("状态注入：%v", err)
	}
	f.FailStatuses(nil)
	f.FailLogin("douyin", boom)
	if _, err := f.Login(ctx, "douyin", services.PlatformLoginOptions{}); !errors.Is(err, boom) {
		t.Fatalf("登录注入：%v", err)
	}
	if list, _ := f.Statuses(ctx); list[0].LoggedIn {
		t.Fatal("失败的登录不应改状态")
	}
	f.FailLogin("douyin", nil)
	f.FailCancel(boom)
	if _, err := f.CancelLogin(ctx, "douyin"); !errors.Is(err, boom) {
		t.Fatalf("取消注入：%v", err)
	}
}

// 停住的登录：finish(nil) 让它成功；CancelLogin 让它以「已取消」结束并返回 true；ctx 取消也能放走；
// 没有进行中的登录时取消返回 false。
func TestPlatformAuthFakeHeldLogin(t *testing.T) {
	f := &PlatformAuth{}
	type result struct {
		st  services.PlatformStatus
		err error
	}
	start := func(ctx context.Context, platform string) chan result {
		out := make(chan result, 1)
		go func() {
			st, err := f.Login(ctx, platform, services.PlatformLoginOptions{})
			out <- result{st, err}
		}()
		waitUntil(t, platform+" 登录停在等待里", func() bool { return f.Pending(platform) })
		return out
	}

	finish := f.HoldLogin("nodeseek")
	done := start(t.Context(), "nodeseek")
	finish(nil)
	finish(errors.New("late")) // 第二次不算数
	if r := <-done; r.err != nil || !r.st.LoggedIn {
		t.Fatalf("finish(nil)：%+v", r)
	}
	if f.Pending("nodeseek") {
		t.Fatal("结束之后不应再是等待中")
	}

	f.HoldLogin("nodeseek")
	done = start(t.Context(), "nodeseek")
	if ok, err := f.CancelLogin(t.Context(), "nodeseek"); err != nil || !ok {
		t.Fatalf("取消进行中的登录：%v %v", ok, err)
	}
	if r := <-done; r.err == nil {
		t.Fatal("被取消的登录应返回错误")
	}
	if ok, _ := f.CancelLogin(t.Context(), "nodeseek"); ok {
		t.Fatal("没有进行中的登录时取消应是 false")
	}

	f.HoldLogin("linuxdo")
	ctx, cancel := context.WithCancel(t.Context())
	done = start(ctx, "linuxdo")
	cancel()
	if r := <-done; !errors.Is(r.err, context.Canceled) {
		t.Fatalf("ctx 取消：%+v", r)
	}
}

// 并发：多个平台同时登录、读状态、取消，-race 下不得报竞态。
func TestPlatformAuthFakeConcurrent(t *testing.T) {
	ctx := t.Context()
	f := &PlatformAuth{}
	platforms := []string{"xiaohongshu", "douyin", "linuxdo", services.PlatformNodeseek}
	var wg sync.WaitGroup
	for _, p := range platforms {
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = f.Login(ctx, p, services.PlatformLoginOptions{})
				_, _ = f.Statuses(ctx)
				_, _ = f.CancelLogin(ctx, p)
				_ = f.Calls()
				_ = f.LoginOptions()
				_ = f.Pending(p)
			}()
		}
	}
	wg.Wait()
	list, _ := f.Statuses(ctx)
	for _, p := range platforms {
		if st, ok := services.FindPlatformStatus(list, p); !ok || !st.LoggedIn {
			t.Errorf("%s 应已登录：%+v", p, list)
		}
	}
}
