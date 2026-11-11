package main

// 对应 web-capture/robots.ts 与 web-network.ts 的 webTextRequest：robots.txt 规则、sitemap 候选链接。
// 规则语义沿用 TS：user-agent 组按"归知"子串匹配、最长路径优先、同长度允许优先、`*` 通配与 `$` 结尾。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/andybalholm/cascadia"
	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/net/html"
)

type robotsRule struct {
	Allow bool
	Path  string
}

type robotsPolicy struct {
	Rules    []robotsRule
	Sitemaps []string
}

type robotsGroup struct {
	agents []string
	rules  []robotsRule
}

// parseRobots 对应 parseRobots。
func parseRobots(text string) robotsPolicy {
	var groups []*robotsGroup
	sitemaps := []string{}
	var group *robotsGroup
	hasDirective := false
	for _, raw := range strings.Split(text, "\n") {
		line := jsTrim(strings.SplitN(raw, "#", 2)[0])
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.ToLower(jsTrim(line[:colon]))
		value := jsTrim(line[colon+1:])
		if key == "sitemap" {
			if len(sitemaps) < 10 {
				sitemaps = append(sitemaps, value)
			}
			continue
		}
		if key == "user-agent" {
			if group == nil || hasDirective {
				group = &robotsGroup{}
				groups = append(groups, group)
				hasDirective = false
			}
			group.agents = append(group.agents, strings.ToLower(value))
		} else if group != nil && (key == "allow" || key == "disallow") {
			hasDirective = true
			if value != "" {
				group.rules = append(group.rules, robotsRule{Allow: key == "allow", Path: value})
			}
		}
	}
	var chosen []*robotsGroup
	for _, g := range groups {
		for _, a := range g.agents {
			// "guizhi".includes(a)：agent 是 guizhi 的子串即视为指向归知（含空串）
			if a != "*" && strings.Contains("guizhi", a) {
				chosen = append(chosen, g)
				break
			}
		}
	}
	if len(chosen) == 0 {
		for _, g := range groups {
			for _, a := range g.agents {
				if a == "*" {
					chosen = append(chosen, g)
					break
				}
			}
		}
	}
	rules := []robotsRule{}
	for _, g := range chosen {
		rules = append(rules, g.rules...)
	}
	return robotsPolicy{Rules: rules, Sitemaps: sitemaps}
}

// robotsAllows 对应 robotsAllows：规则按路径+查询串匹配。
func robotsAllows(rawURL string, policy robotsPolicy) bool {
	u, err := whatwg.Parse(rawURL)
	if err != nil {
		return true
	}
	path := u.Pathname() + u.Search()
	best, allowed := -1, true
	for _, rule := range policy.Rules {
		end := strings.HasSuffix(rule.Path, "$")
		value := rule.Path
		if end {
			value = value[:len(value)-1]
		}
		parts := strings.Split(value, "*")
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		pattern := "^" + strings.Join(parts, ".*")
		if end {
			pattern += "$"
		}
		re, err := regexp.Compile(pattern)
		if err != nil || !re.MatchString(path) {
			continue
		}
		length := utf16Len(strings.ReplaceAll(value, "*", ""))
		if length > best || (length == best && rule.Allow) {
			best, allowed = length, rule.Allow
		}
	}
	return allowed
}

type webTextResponse struct {
	Status int
	Text   string
	URL    string
}

// webTextRequest 对应 webTextRequest：robots/sitemap 与渲染窗口同一身份，只带 UA。
func (f *safeFetcher) webTextRequest(ctx context.Context, raw string, allowed func(string) bool) (webTextResponse, error) {
	url, err := canonicalWebURL(raw)
	if err != nil {
		return webTextResponse{}, err
	}
	opts := webIdentityOptions()
	opts.Accept = "*/*"
	for hop := 0; hop <= webMaxHops; hop++ {
		if !allowed(url) {
			return webTextResponse{}, errors.New("重定向超出允许范围")
		}
		resp, err := f.webRequest(ctx, url, opts)
		if err != nil {
			return webTextResponse{}, err
		}
		if loc := resp.Headers["location"]; loc != "" && (resp.Status == 301 || resp.Status == 302 || resp.Status == 303 || resp.Status == 307 || resp.Status == 308) {
			next, err := whatwg.ParseRef(url, loc)
			if err != nil {
				return webTextResponse{}, errors.New("Invalid URL")
			}
			if url, err = canonicalWebURL(next.Href(false)); err != nil {
				return webTextResponse{}, err
			}
			continue
		}
		return webTextResponse{Status: resp.Status, Text: lossyUTF8(resp.Body), URL: url}, nil
	}
	return webTextResponse{}, errors.New("网页重定向超过 5 次")
}

var reLeadingMarkup = regexp.MustCompile(`^[` + jsSpaceClass + `]*<`)

// loadRobots 对应 loadRobots。
func (f *safeFetcher) loadRobots(ctx context.Context, origin string) (robotsPolicy, error) {
	res, err := f.webTextRequest(ctx, origin+"/robots.txt", func(u string) bool { return webOrigin(u) == origin })
	if err != nil {
		return robotsPolicy{}, err
	}
	if res.Status == 404 || res.Status == 410 {
		return robotsPolicy{Rules: []robotsRule{}, Sitemaps: []string{}}, nil
	}
	if res.Status < 200 || res.Status >= 300 {
		return robotsPolicy{}, fmt.Errorf("robots.txt 读取失败（HTTP %d），已暂停来源", res.Status)
	}
	if utf16Len(res.Text) > 512000 || reLeadingMarkup.MatchString(res.Text) {
		return robotsPolicy{}, errors.New("robots.txt 不是有效规则文件，已暂停来源")
	}
	return parseRobots(res.Text), nil
}

var selSitemapLoc = cascadia.MustCompile("url > loc")

// sitemapLocs 对应 `parseHTML(text).document.querySelectorAll("url > loc").slice(0,300)` 取 trim 后的文本。
func sitemapLocs(text string) []string {
	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return nil
	}
	out := []string{}
	for _, n := range cascadia.QueryAll(doc, selSitemapLoc) {
		if len(out) == 300 {
			break
		}
		out = append(out, jsTrim(nodeText(n)))
	}
	return out
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}
