# 复制 go-readability v2 到 .patched/ 并打上产品对 Readability.js 的两处改动：
#   1. linkDensityModifier：条件清理里的链接占比阈值 0.2 / 0.5 加上该修正值（对应 JS 的 options.linkDensityModifier）
#   2. 保留与标题重复的首个 h1/h2（对应产品把 _headerDuplicatesTitle 改成恒 false）
# 用法：powershell -File prepare-patched.ps1 ；之后 go run -tags patched -modfile=go.patched.mod . <样本目录> <golden 目录> product
$ErrorActionPreference = "Stop"
$src = (go list -m -f '{{.Dir}}' codeberg.org/readeck/go-readability/v2).Trim()
$dst = Join-Path $PSScriptRoot ".patched\readability"
Remove-Item $dst -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $dst | Out-Null
Copy-Item "$src\*" $dst -Recurse -Force
Get-ChildItem $dst -Recurse -File | ForEach-Object { $_.IsReadOnly = $false }
$file = Join-Path $dst "parser.go"
$t = [IO.File]::ReadAllText($file)
function Swap($text, $old, $new) {
  if (($text.Split([string[]]@($old), [StringSplitOptions]::None).Count - 1) -ne 1) { throw "补丁位置不唯一或不存在：$old" }
  return $text.Replace($old, $new)
}
$t = Swap $t '	AllowedVideoRegex *regexp.Regexp' ("	AllowedVideoRegex *regexp.Regexp`n	// LinkDensityModifier 对应 Readability.js 的 options.linkDensityModifier。`n	LinkDensityModifier float64`n	// KeepTitleHeader 为真时不删除与标题重复的首个 h1/h2。`n	KeepTitleHeader bool")
$t = Swap $t 'weight < 25 && linkDensity > 0.2 {' 'weight < 25 && linkDensity > 0.2+ps.LinkDensityModifier {'
$t = Swap $t 'weight >= 25 && linkDensity > 0.5 {' 'weight >= 25 && linkDensity > 0.5+ps.LinkDensityModifier {'
$t = Swap $t 'shouldRemoveTitleHeader && ps.headerDuplicatesTitle(node) {' 'shouldRemoveTitleHeader && !ps.KeepTitleHeader && ps.headerDuplicatesTitle(node) {'
[IO.File]::WriteAllText($file, $t)
# 独立的 modfile：replace 到打过补丁的副本，不影响默认构建。
$mod = [IO.File]::ReadAllText((Join-Path $PSScriptRoot "go.mod")) + "`nreplace codeberg.org/readeck/go-readability/v2 => ./.patched/readability`n"
[IO.File]::WriteAllText((Join-Path $PSScriptRoot "go.patched.mod"), $mod)
Copy-Item (Join-Path $PSScriptRoot "go.sum") (Join-Path $PSScriptRoot "go.patched.sum") -Force
Write-Host "已生成 .patched/readability 与 go.patched.mod"
