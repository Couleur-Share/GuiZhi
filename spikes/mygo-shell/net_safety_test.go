package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type netBaseGolden struct {
	Net struct {
		Addr []struct {
			A         string `json:"a"`
			Priv      bool   `json:"priv"`
			Forbidden bool   `json:"forbidden"`
		} `json:"addr"`
		Blocked []struct {
			H       string `json:"h"`
			Blocked bool   `json:"blocked"`
		} `json:"blocked"`
		Resolve []struct {
			Host    string `json:"host"`
			Opt     int    `json:"opt"`
			OK      bool   `json:"ok"`
			Address string `json:"address"`
			Family  int    `json:"family"`
			Error   string `json:"error"`
		} `json:"resolve"`
		Normalize []struct {
			Input    any                  `json:"input"`
			Settings networkProxySettings `json:"settings"`
			URL      *string              `json:"url"`
		} `json:"normalize"`
		Bypass []struct {
			Rules string `json:"rules"`
			Host  string `json:"host"`
			R     bool   `json:"r"`
		} `json:"bypass"`
	} `json:"net"`
}

func loadNetBaseGolden(t *testing.T) netBaseGolden {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g netBaseGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Net.Addr) == 0 {
		t.Fatal("golden 缺少 net 段")
	}
	return g
}

// 已知差异（Go 更严，见 net_safety.go 头注释）：点分兼容写法 TS 漏判。corpus 里没有这类输入，保持空表以便新增时显式登记。
var netAddrAccepted = map[string]string{}

func TestNetSafetyAddressesMatchTS(t *testing.T) {
	g := loadNetBaseGolden(t)
	for _, c := range g.Net.Addr {
		if why, ok := netAddrAccepted[c.A]; ok {
			t.Logf("跳过 %q：%s", c.A, why)
			continue
		}
		if got := isPrivateAddress(c.A); got != c.Priv {
			t.Errorf("isPrivateAddress(%q)=%v，TS=%v", c.A, got, c.Priv)
		}
		a, fam := parseIPLiteral(c.A)
		forbidden := fam != 0 && isForbiddenAIEndpointAddr(a)
		if forbidden != c.Forbidden {
			t.Errorf("isForbiddenAIEndpointAddr(%q)=%v，TS=%v", c.A, forbidden, c.Forbidden)
		}
	}
	for _, c := range g.Net.Blocked {
		if got := isBlockedHostname(c.H); got != c.Blocked {
			t.Errorf("isBlockedHostname(%q)=%v，TS=%v", c.H, got, c.Blocked)
		}
	}
}

func TestResolvePublicAddressLiteralsMatchTS(t *testing.T) {
	g := loadNetBaseGolden(t)
	sets := []resolveOptions{
		{},
		{AllowPrivateNetwork: true},
		{AllowProxyCompatibilityAddress: true},
		{AllowPrivateNetwork: true, AllowProxyCompatibilityAddress: true},
	}
	noLookup := func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("不应发起 DNS 查询") }
	for _, c := range g.Net.Resolve {
		// 已接受差异：点分写法的映射 fake-ip 字面量（::ffff:198.18.0.1）TS 因按十六进制误解析而拒绝，Go 按真实字节放行（仅在已配置代理时）
		if c.Host == "::ffff:198.18.0.1" && !c.OK && sets[c.Opt].AllowProxyCompatibilityAddress {
			continue
		}
		got, err := resolvePublicAddress(context.Background(), c.Host, sets[c.Opt], noLookup)
		if (err == nil) != c.OK {
			t.Errorf("resolve(%q, opt%d)：Go err=%v，TS ok=%v（%s）", c.Host, c.Opt, err, c.OK, c.Error)
			continue
		}
		if err != nil {
			if err.Error() != c.Error {
				t.Errorf("resolve(%q, opt%d) 错误文本：Go=%q TS=%q", c.Host, c.Opt, err.Error(), c.Error)
			}
			continue
		}
		if got.Address != c.Address || got.Family != c.Family {
			t.Errorf("resolve(%q, opt%d)：Go=%+v TS=%s/%d", c.Host, c.Opt, got, c.Address, c.Family)
		}
	}
}

func TestResolvePublicAddressWithDNS(t *testing.T) {
	look := func(m map[string][]string) lookupFunc {
		return func(_ context.Context, host string) ([]netip.Addr, error) {
			var out []netip.Addr
			for _, s := range m[host] {
				out = append(out, netip.MustParseAddr(s))
			}
			return out, nil
		}
	}
	ctx := context.Background()
	cases := []struct {
		name    string
		dns     map[string][]string
		host    string
		opts    resolveOptions
		want    string
		wantErr string
	}{
		{"公网", map[string][]string{"a.test": {"93.184.216.34"}}, "a.test", resolveOptions{}, "93.184.216.34", ""},
		{"取第一条", map[string][]string{"a.test": {"1.1.1.1", "2.2.2.2"}}, "a.test", resolveOptions{}, "1.1.1.1", ""},
		{"任一私网即拒（防 DNS rebinding 混合应答）", map[string][]string{"a.test": {"1.1.1.1", "10.0.0.5"}}, "a.test", resolveOptions{}, "", "Access to internal network addresses is not allowed"},
		{"允许私网", map[string][]string{"a.test": {"10.0.0.5"}}, "a.test", resolveOptions{AllowPrivateNetwork: true}, "10.0.0.5", ""},
		{"fake-ip 无代理配置→提示", map[string][]string{"a.test": {"198.18.0.7"}}, "a.test", resolveOptions{}, "", "解析到 198.18.0.7"},
		{"fake-ip 有代理配置→放行", map[string][]string{"a.test": {"198.18.0.7"}}, "a.test", resolveOptions{AllowProxyCompatibilityAddress: true}, "198.18.0.7", ""},
		{"fake-ip 混入真私网→拒", map[string][]string{"a.test": {"198.18.0.7", "10.0.0.1"}}, "a.test", resolveOptions{AllowProxyCompatibilityAddress: true}, "", "Access to internal network addresses is not allowed"},
		{"空应答", map[string][]string{}, "a.test", resolveOptions{}, "", "Failed to resolve remote host"},
		{"本地主机名", nil, "x.localhost", resolveOptions{}, "", "Access to local network addresses is not allowed"},
	}
	for _, c := range cases {
		got, err := resolvePublicAddress(ctx, c.host, c.opts, look(c.dns))
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s：期望错误含 %q，实际 %v", c.name, c.wantErr, err)
			}
			continue
		}
		if err != nil || got.Address != c.want {
			t.Errorf("%s：got=%+v err=%v，期望 %s", c.name, got, err, c.want)
		}
	}
}

func TestNetworkProxyNormalizeMatchesTS(t *testing.T) {
	g := loadNetBaseGolden(t)
	for i, c := range g.Net.Normalize {
		input := c.Input
		if s, ok := input.(string); ok && s == "__undefined__" {
			input = nil
		}
		got := normalizeNetworkProxy(input)
		if !reflect.DeepEqual(got, c.Settings) {
			t.Errorf("#%d normalize(%v)：\n Go=%+v\n TS=%+v", i, c.Input, got, c.Settings)
			continue
		}
		wantURL := ""
		if c.URL != nil {
			wantURL = *c.URL
		}
		if u := buildProxyURL(got); u != wantURL {
			t.Errorf("#%d buildProxyURL(%v)：Go=%q TS=%q", i, c.Input, u, wantURL)
		}
	}
}

func TestProxyBypassMatchesTS(t *testing.T) {
	g := loadNetBaseGolden(t)
	for _, c := range g.Net.Bypass {
		if got := shouldBypassProxy(c.Host, c.Rules); got != c.R {
			t.Errorf("shouldBypassProxy(%q, %q)=%v，TS=%v", c.Host, c.Rules, got, c.R)
		}
	}
}
