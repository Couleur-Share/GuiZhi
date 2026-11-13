# 迁移进度盘点：按 ipc-channels.ts 与 apps/desktop-go 源码里出现的频道字符串统计
# 用法：在仓库根目录 .\spikes\migration-status.ps1 [-List]
param([switch]$List)
$ts = Get-Content packages\shared\constants\ipc-channels.ts -Raw
$pairs = [regex]::Matches($ts, '^\s*([A-Z0-9_]+):\s*["'']([^"'']+)["'']', 'Multiline') | % { $_.Groups[2].Value }
$go = (Get-ChildItem apps\desktop-go\*.go -Exclude *_test.go | Get-Content -Raw) -join "`n"
$frozen = '^(research|illustration|discovery|discoveryView|discoveryCandidate|readingGraphics|mobile-capture|reading-sync):'
$rows = $pairs | % { [pscustomobject]@{ Ch = $_; Done = $go.Contains('"' + $_ + '"'); Frozen = ($_ -match $frozen) } }
$todo = $rows | ? { -not $_.Done -and -not $_.Frozen }
"total=$($rows.Count) done=$(($rows|?{$_.Done}).Count) frozen=$(($rows|?{$_.Frozen -and -not $_.Done}).Count) todo=$($todo.Count)"
$todo | Group-Object { ($_.Ch -split ':')[0] } | Sort-Object Count -Descending | % { "{0,-20} {1}" -f $_.Name, $_.Count }
if ($List) { $todo | % { $_.Ch } }

# 追加：ipc-channels.ts 之外直接以字符串字面量注册的频道（ipcMain.handle/on("x:y")）
$known = [System.Collections.Generic.HashSet[string]]::new([string[]]$pairs)
$lit = Get-ChildItem apps\desktop\src\main -Recurse -Include *.ts | ? { $_.Name -notmatch '\.test\.' } | % {
  [regex]::Matches((Get-Content $_.FullName -Raw), 'ipcMain\.(?:handle|on|once)\(\s*["'']([A-Za-z0-9_:\-]+)["'']') | % { $_.Groups[1].Value }
} | Sort-Object -Unique | ? { -not $known.Contains($_) }
$litRows = $lit | % { [pscustomobject]@{ Ch = $_; Done = $go.Contains('"' + $_ + '"'); Frozen = ($_ -match $frozen) } }
$litTodo = $litRows | ? { -not $_.Done -and -not $_.Frozen }
"literal(outside constants): total=$($litRows.Count) done=$(($litRows|?{$_.Done}).Count) frozen=$(($litRows|?{$_.Frozen -and -not $_.Done}).Count) todo=$($litTodo.Count)"
if ($List) { $litTodo | % { 'literal: ' + $_.Ch } }
