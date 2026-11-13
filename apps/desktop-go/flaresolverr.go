package main

// 对应 services/web-capture/flaresolverr-connection.ts 与 flaresolverr.ts 中的连接与控制接口部分：
// 本地直连，或经已有 SSH 配置起一条仅回环的隧道；控制命令只发往 127.0.0.1:<port>/v1，且不经过全局代理。
// 后备采集（captureWithFlareSolverr）随渲染/验证切片接入，这里只提供连接、命令与 web:solverCheck 所需的连通性检查。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// flareSolverrConn 对应 connectFlareSolverr 的返回值：endpoint 是回环控制接口，close 关闭自己起的 SSH 隧道。
type flareSolverrConn struct {
	Endpoint string
	Close    func()
}

// flareSolverrDirectClient 控制接口直连：不走任何代理，也不复用连接（对应 http.request 的 agent:false）。
var flareSolverrDirectClient = &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}

func reserveLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return port, ln.Close()
}

func loopbackPortReady(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// connectFlareSolverr 只启动自己的回环隧道；不改 SSH 配置、不关闭用户已有连接。
func connectFlareSolverr(ctx context.Context, settings flareSolverrSettings) (*flareSolverrConn, error) {
	if ctx.Err() != nil {
		return nil, webAbortFailure(ctx)
	}
	if settings.Connection == "local" {
		return &flareSolverrConn{Endpoint: fmt.Sprintf("http://127.0.0.1:%d/v1", settings.Port), Close: func() {}}, nil
	}
	if settings.SSHHost == "" {
		return nil, errors.New("请填写已有 SSH 配置中的主机别名")
	}
	port, err := reserveLoopbackPort()
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, webAbortFailure(ctx)
	}
	cmd := exec.Command("ssh",
		"-N", "-T",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", port, settings.Port),
		settings.SSHHost,
	)
	hideChildWindow(cmd)
	// 不保留 SSH 原始输出，避免配置中的账号信息进入日志。
	cmd.Stdout, cmd.Stderr, cmd.Stdin = io.Discard, io.Discard, nil
	if err := cmd.Start(); err != nil {
		return nil, errors.New("FlareSolverr SSH 连接失败，请检查主机别名、密钥和服务器端口")
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	var once sync.Once
	closeFn := func() {
		once.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	for attempt := 0; attempt < 100; attempt++ {
		if ctx.Err() != nil {
			closeFn()
			return nil, webAbortFailure(ctx)
		}
		select {
		case <-exited:
			return nil, errors.New("FlareSolverr SSH 连接失败，请检查主机别名、密钥和服务器端口")
		default:
		}
		if loopbackPortReady(port) {
			return &flareSolverrConn{Endpoint: fmt.Sprintf("http://127.0.0.1:%d/v1", port), Close: closeFn}, nil
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			closeFn()
			return nil, webAbortFailure(ctx)
		}
	}
	closeFn()
	return nil, errors.New("FlareSolverr SSH 连接超时")
}

var (
	reFlareTimeout  = regexp.MustCompile(`(?i)timeout|timed out`)
	reFlareEndpoint = regexp.MustCompile(`^http://127\.0\.0\.1(:\d+)?/v1$`)
)

// flareSolverrCommand 回环控制接口直连；从不向提取器或日志传递 Cookie。
func flareSolverrCommand(ctx context.Context, endpoint string, input any) (map[string]any, error) {
	if !reFlareEndpoint.MatchString(endpoint) {
		return nil, errors.New("FlareSolverr 控制接口只允许回环地址")
	}
	if ctx.Err() != nil {
		return nil, webAbortFailure(ctx)
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := flareSolverrDirectClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, webAbortFailure(ctx)
		}
		return nil, errors.New("无法连接 FlareSolverr，请检查服务是否启动")
	}
	defer resp.Body.Close()
	const limit = 16 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, webAbortFailure(ctx)
		}
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("FlareSolverr 响应超过 16 MiB")
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil || result == nil {
		return nil, errors.New("FlareSolverr 返回了无效响应")
	}
	if resp.StatusCode != http.StatusOK || result["status"] != "ok" {
		// 上游错误可能携带 Cookie 或完整请求，不能原样展示。
		if reFlareTimeout.MatchString(jsToString(result["message"])) {
			return nil, errors.New("FlareSolverr 验证超时（60 秒），请稍后重试")
		}
		return nil, errors.New("FlareSolverr 未能完成请求，请检查服务状态或稍后重试")
	}
	return result, nil
}

// checkFlareSolverrConnection 对应 checkFlareSolverrConnection：25 秒内连上并列出会话即视为连通。
func checkFlareSolverrConnection(ctx context.Context, settings flareSolverrSettings) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	conn, err := connectFlareSolverr(ctx, settings)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := flareSolverrCommand(ctx, conn.Endpoint, map[string]any{"cmd": "sessions.list"}); err != nil {
		return nil, err
	}
	return map[string]any{"connected": true}, nil
}
