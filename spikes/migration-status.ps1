# 迁移进度盘点：按 ipc-channels.ts 与 mygo-shell 源码里出现的频道字符串统计
# 用法：在仓库根目录 .\spikes\migration-status.ps1 [-List]
param([switch]$List)
$ts = Get-Content packages\shared\constants\ipc-channels.ts -Raw
$pairs = [regex]::Matches($ts, '^\s*([A-Z0-9_]+):\s*["'']([^"'']+)["'']', 'Multiline') | % { $_.Groups[2].Value }
$go = (Get-ChildItem spikes\mygo-shell\*.go -Exclude *_test.go | Get-Content -Raw) -join "`n"
$frozen = '^(research|illustration|discovery|discoveryView|discoveryCandidate|readingGraphics|mobile-capture|reading-sync):'
$rows = $pairs | % { [pscustomobject]@{ Ch = $_; Done = $go.Contains('"' + $_ + '"'); Frozen = ($_ -match $frozen) } }
$todo = $rows | ? { -not $_.Done -and -not $_.Frozen }
"total=$($rows.Count) done=$(($rows|?{$_.Done}).Count) frozen=$(($rows|?{$_.Frozen -and -not $_.Done}).Count) todo=$($todo.Count)"
$todo | Group-Object { ($_.Ch -split ':')[0] } | Sort-Object Count -Descending | % { "{0,-20} {1}" -f $_.Name, $_.Count }
if ($List) { $todo | % { $_.Ch } }
