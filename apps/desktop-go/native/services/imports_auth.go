package services

import "context"

// 采集页「登录态重试」与「NodeSeek 验证」依赖的平台账号能力（渲染层 window.api.platformCapture 的 getStatuses / login / cancelLogin）。
//
// 设置页「采集与转写」的 CaptureTools.PlatformLogin 只带平台与强制重新登录两个参数；
// NodeSeek 的验证要把「受阻帖子的地址」交给登录窗口（platformCapture:login 的 verificationUrl），所以这里单独给一个窄接口。
// 适配器（package main 的 native_imports_ext.go）直连现有处理器；返回的状态里没有 Cookie、令牌之类的凭据。

// PlatformNodeseek 是 NodeSeek 的平台取值：它的「登录」是人机验证，不是账号登录。
const PlatformNodeseek = "nodeseek"

// PlatformLoginOptions 是登录窗口的可选参数；零值表示普通登录（复用已有会话）。
type PlatformLoginOptions struct {
	// ForceRelogin 先清掉旧会话再登录。
	ForceRelogin bool
	// VerificationURL 只对 NodeSeek 有意义：要验证的帖子地址，处理器会再校验只接受 NodeSeek 的帖子页。
	VerificationURL string
}

// PlatformAuth 是采集页依赖的平台账号能力。
type PlatformAuth interface {
	// Statuses 返回各平台账号的状态（platformCapture:status）。
	Statuses(ctx context.Context) ([]PlatformStatus, error)
	// Login 打开内置登录窗口并等待结果（可能几分钟，随 ctx 取消）；返回登录后的状态。
	Login(ctx context.Context, platform string, opts PlatformLoginOptions) (PlatformStatus, error)
	// CancelLogin 取消正在进行的登录或验证；没有可取消的返回 false。
	CancelLogin(ctx context.Context, platform string) (bool, error)
}

// FindPlatformStatus 在状态列表里找一个平台；没有返回 false。
func FindPlatformStatus(list []PlatformStatus, platform string) (PlatformStatus, bool) {
	for _, s := range list {
		if s.Platform == platform {
			return s, true
		}
	}
	return PlatformStatus{}, false
}
