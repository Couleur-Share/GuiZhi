package main

// 更新器的纯逻辑：版本比较、更新通道选源、CHANGELOG 区间截取。
// 一一对应 utils/version.ts、main/updater-channel.ts、utils/changelog.ts，由 golden-updater.mts 差分。

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type parsedVersion struct {
	major, minor, patch float64
	prerelease          []any // float64 或 string
}

var reSemver = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

func normalizeVersion(v string) string {
	s := jsTrim(v)
	s = strings.TrimPrefix(s, "v")
	if i := strings.Index(s, "+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "0.0.0"
	}
	return s
}

func parseVersion(v string) *parsedVersion {
	m := reSemver.FindStringSubmatch(normalizeVersion(v))
	if m == nil {
		return nil
	}
	num := func(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
	p := &parsedVersion{major: num(m[1]), minor: num(m[2]), patch: num(m[3])}
	if m[4] != "" {
		for _, id := range strings.Split(m[4], ".") {
			if id != "" && strings.Trim(id, "0123456789") == "" {
				p.prerelease = append(p.prerelease, num(id))
			} else {
				p.prerelease = append(p.prerelease, id)
			}
		}
	}
	return p
}

func comparePrereleaseIdentifiers(l, r any) int {
	lf, lIsNum := l.(float64)
	rf, rIsNum := r.(float64)
	switch {
	case lIsNum && rIsNum:
		switch {
		case lf == rf:
			return 0
		case lf > rf:
			return 1
		}
		return -1
	case lIsNum:
		return -1
	case rIsNum:
		return 1
	}
	ls, rs := l.(string), r.(string)
	switch {
	case ls == rs:
		return 0
	case ls > rs:
		return 1
	}
	return -1
}

// naturalCompare 近似 localeCompare(…, {numeric: true})：只用于无法按 semver 解析的版本串兜底。
// 已知差异：ICU 的字母序规则（大小写、标点权重）这里只做大小写不敏感的逐字比较。
func naturalCompare(a, b string) int {
	chunks := func(s string) []string {
		var out []string
		for len(s) > 0 {
			isDigit := s[0] >= '0' && s[0] <= '9'
			i := 1
			for i < len(s) && (s[i] >= '0' && s[i] <= '9') == isDigit {
				i++
			}
			out = append(out, s[:i])
			s = s[i:]
		}
		return out
	}
	ac, bc := chunks(a), chunks(b)
	for i := 0; i < len(ac) && i < len(bc); i++ {
		x, y := ac[i], bc[i]
		xd, yd := x[0] >= '0' && x[0] <= '9', y[0] >= '0' && y[0] <= '9'
		if xd && yd {
			xf, _ := strconv.ParseFloat(x, 64)
			yf, _ := strconv.ParseFloat(y, 64)
			if xf != yf {
				if xf > yf {
					return 1
				}
				return -1
			}
			continue
		}
		if c := strings.Compare(strings.ToLower(x), strings.ToLower(y)); c != 0 {
			return c
		}
	}
	switch {
	case len(ac) > len(bc):
		return 1
	case len(ac) < len(bc):
		return -1
	}
	return 0
}

func compareVersions(left, right string) int {
	l, r := parseVersion(left), parseVersion(right)
	if l == nil || r == nil {
		nl, nr := normalizeVersion(left), normalizeVersion(right)
		if nl == nr {
			return 0
		}
		return naturalCompare(nl, nr)
	}
	for _, pair := range [][2]float64{{l.major, r.major}, {l.minor, r.minor}, {l.patch, r.patch}} {
		if pair[0] > pair[1] {
			return 1
		}
		if pair[0] < pair[1] {
			return -1
		}
	}
	switch {
	case len(l.prerelease) == 0 && len(r.prerelease) == 0:
		return 0
	case len(l.prerelease) == 0:
		return 1
	case len(r.prerelease) == 0:
		return -1
	}
	for i := 0; i < max(len(l.prerelease), len(r.prerelease)); i++ {
		if i >= len(l.prerelease) {
			return -1
		}
		if i >= len(r.prerelease) {
			return 1
		}
		if c := comparePrereleaseIdentifiers(l.prerelease[i], r.prerelease[i]); c != 0 {
			return c
		}
	}
	return 0
}

func isPrereleaseVersion(v string) bool {
	p := parseVersion(v)
	return p != nil && len(p.prerelease) > 0
}

func getReleaseTagForVersion(v string) string {
	n := normalizeVersion(v)
	if strings.HasPrefix(n, "v") {
		return n
	}
	return "v" + n
}

// ── 更新通道选源（updater-channel.ts）──

type githubReleaseCandidate struct {
	Draft      any `json:"draft"`
	Prerelease any `json:"prerelease"`
	TagName    any `json:"tag_name"`
}

type previewFeedChoice struct {
	TagName    string `json:"tagName"`
	Prerelease bool   `json:"prerelease"`
	Version    string `json:"version"`
}

// pickPreviewFeedRelease：正式版与 prerelease 都参与比较，版本更高者胜出；同版本偏好正式版。
func pickPreviewFeedRelease(releases []githubReleaseCandidate) *previewFeedChoice {
	var best *previewFeedChoice
	for _, r := range releases {
		if d, _ := r.Draft.(bool); d {
			continue
		}
		tag, ok := r.TagName.(string)
		if !ok || jsTrim(tag) == "" {
			continue
		}
		tag = jsTrim(tag)
		pre, _ := r.Prerelease.(bool)
		cand := &previewFeedChoice{TagName: tag, Prerelease: pre, Version: normalizeVersion(tag)}
		if best == nil {
			best = cand
			continue
		}
		cmp := compareVersions(cand.Version, best.Version)
		if cmp > 0 || (cmp == 0 && best.Prerelease && !cand.Prerelease) {
			best = cand
		}
	}
	return best
}

// releaseTagForPreviewFeed：赢家是 prerelease 才钉 tag，否则走 latest/download。
func releaseTagForPreviewFeed(c *previewFeedChoice) string {
	if c == nil || !c.Prerelease {
		return ""
	}
	return c.TagName
}

// ── CHANGELOG 截取（changelog.ts）──

type changelogSection struct {
	Version string
	Start   int // 字节偏移
}

var reChangelogHeading = regexp.MustCompile(`(?:^|[\n\r\x{2028}\x{2029}])(## \[?v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)\]?)`)

func parseChangelogVersions(content string) []changelogSection {
	var out []changelogSection
	for _, m := range reChangelogHeading.FindAllStringSubmatchIndex(content, -1) {
		out = append(out, changelogSection{Version: content[m[4]:m[5]], Start: m[2]})
	}
	return out
}

// stripDashLines 对应 .replace(/^---\s*$/gm, "")：多行模式，`\s*` 会越过换行，取能让 `$` 成立的最长一段。
func stripDashLines(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		atLineStart := i == 0
		if !atLineStart {
			r, _ := utf8.DecodeLastRuneInString(s[:i])
			atLineStart = isJSLineTerminator(r)
		}
		if atLineStart && strings.HasPrefix(s[i:], "---") {
			// 空白串的所有候选终点（rune 边界），从最长往短试，第一个满足 `$`（串尾或下一个字符是行终止符）的就是匹配终点
			j := i + 3
			bounds := []int{j}
			for k := j; k < len(s); {
				r, size := utf8.DecodeRuneInString(s[k:])
				if !jsSpace(r) {
					break
				}
				k += size
				bounds = append(bounds, k)
			}
			matched := false
			for n := len(bounds) - 1; n >= 0; n-- {
				p := bounds[n]
				if p == len(s) {
					i, matched = p, true
					break
				}
				if r, _ := utf8.DecodeRuneInString(s[p:]); isJSLineTerminator(r) {
					i, matched = p, true
					break
				}
			}
			if matched {
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func sliceChangelogSection(content string, sections []changelogSection, i int) string {
	end := len(content)
	if i+1 < len(sections) {
		end = sections[i+1].Start
	}
	return jsTrim(stripDashLines(content[sections[i].Start:end]))
}

func extractLatestChangelogSection(content string) string {
	s := parseChangelogVersions(content)
	if len(s) == 0 {
		return ""
	}
	return sliceChangelogSection(content, s, 0)
}

// extractChangelogRange 取 (currentVersion, newVersion] 区间内的版本记录，多节用分隔线拼接。
func extractChangelogRange(content, newVersion, currentVersion string) string {
	s := parseChangelogVersions(content)
	var relevant []string
	for i, sec := range s {
		if compareVersions(sec.Version, currentVersion) > 0 && compareVersions(sec.Version, newVersion) <= 0 {
			relevant = append(relevant, sliceChangelogSection(content, s, i))
		}
	}
	return strings.Join(relevant, "\n\n---\n\n")
}
