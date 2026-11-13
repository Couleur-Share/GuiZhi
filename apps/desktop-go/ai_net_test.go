package main

// 守卫移植的差分测试：golden 由 spikes/mygo-ai/make-net-golden.mts 用真实 Node（WHATWG URL）与产品 TS 代码生成。
// 重新生成：pnpm dlx tsx spikes/mygo-ai/make-net-golden.mts apps/desktop-go/testdata/ai-net.json

import (
	"encoding/json"
	"net/netip"
	"os"
	"strings"
	"testing"
)

type netGolden struct {
	URLs []struct {
		In       string `json:"in"`
		OK       bool   `json:"ok"`
		Protocol string `json:"protocol"`
		Host     string `json:"host"`
		Hostname string `json:"hostname"`
	} `json:"urls"`
	Addrs []struct {
		Addr      string `json:"addr"`
		Forbidden bool   `json:"forbidden"`
	} `json:"addrs"`
	Gate []struct {
		URL        string   `json:"url"`
		Now        int64    `json:"now"`
		Last       int64    `json:"last"`
		Granted    []string `json:"granted"`
		Configured []string `json:"configured"`
		Result     struct {
			Allowed bool    `json:"allowed"`
			Reason  *string `json:"reason"`
			Host    *string `json:"host"`
			IsKnown bool    `json:"isKnown"`
		} `json:"result"`
	} `json:"gate"`
}

func loadNetGolden(t *testing.T) netGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/ai-net.json")
	if err != nil {
		t.Fatal(err)
	}
	var g netGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestEndpointURLMatchesWHATWG(t *testing.T) {
	g := loadNetGolden(t)
	bad := 0
	for _, c := range g.URLs {
		u, err := parseEndpointURL(c.In)
		switch {
		case !c.OK && err == nil:
			// 非 http(s) 协议在 TS 里是合法 URL；这里 err==nil 且 Hostname 为空才算“解析出了协议”
			t.Errorf("%q: Node 判无效，Go 判有效（%+v）", c.In, u)
			bad++
		case c.OK && err != nil:
			t.Errorf("%q: Node 判有效，Go 判无效", c.In)
			bad++
		case c.OK:
			if u.Protocol != c.Protocol {
				t.Errorf("%q: protocol Go=%q Node=%q", c.In, u.Protocol, c.Protocol)
				bad++
			} else if (c.Protocol == "http:" || c.Protocol == "https:") && (u.Host != c.Host || u.Hostname != c.Hostname) {
				t.Errorf("%q: host Go=%q/%q Node=%q/%q", c.In, u.Host, u.Hostname, c.Host, c.Hostname)
				bad++
			}
		}
	}
	t.Logf("URL 解析 %d 条，不一致 %d 条", len(g.URLs), bad)
}

func TestForbiddenAddrMatchesTS(t *testing.T) {
	g := loadNetGolden(t)
	for _, c := range g.Addrs {
		a, err := netip.ParseAddr(c.Addr)
		if err != nil {
			t.Errorf("%q 无法解析", c.Addr)
			continue
		}
		if got := isForbiddenAIEndpointAddr(a); got != c.Forbidden {
			t.Errorf("%q: Go=%v TS=%v", c.Addr, got, c.Forbidden)
		}
	}
	t.Logf("地址判定 %d 条", len(g.Addrs))
}

func TestEndpointGateMatchesTS(t *testing.T) {
	g := loadNetGolden(t)
	set := func(l []string) map[string]bool {
		m := map[string]bool{}
		for _, s := range l {
			m[s] = true
		}
		return m
	}
	for _, c := range g.Gate {
		got := evaluateEndpointAccess(c.URL, c.Now, set(c.Configured), set(c.Granted), c.Last)
		want := c.Result
		wantReason := ""
		if want.Reason != nil {
			wantReason = *want.Reason
		}
		wantHost := ""
		if want.Host != nil {
			wantHost = *want.Host
		}
		if got.Allowed != want.Allowed || got.Reason != wantReason || got.Host != wantHost || (got.Allowed && got.IsKnown != want.IsKnown) {
			t.Errorf("%s now=%d last=%d granted=%s: Go=%+v TS=%+v", c.URL, c.Now, c.Last, strings.Join(c.Granted, ","), got, want)
		}
	}
	t.Logf("准入判定 %d 条", len(g.Gate))
}
