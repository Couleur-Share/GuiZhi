# 主题阅读视图 iframe 真实 UI 往返（离屏、隔离沙箱）。
# 先用 TS 夹具脚本在黄金库副本里加一个带 v3 阅读版本的条目，再离屏启动 GuiZhi.exe 跑 rv-roundtrip.js。
param([string]$Db = (Join-Path $env:TEMP 'guizhi-golden\knowledge.db'))
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"
$repo = (Resolve-Path "$PSScriptRoot\..\..\..").Path
$sb = New-Sandbox -Name 'reading-view'
Remove-Item "$($sb.Root)\ud" -Recurse -Force
Push-Location $repo
try { pnpm dlx tsx apps\desktop-go\verify\make-reading-fixture.mts $Db "$($sb.Root)\ud" 2>&1 | Select-String '^\{' | ForEach-Object { $_.ToString() } } finally { Pop-Location }
$r = Invoke-Shot -Sandbox $sb -ScriptFile "$PSScriptRoot\rv-roundtrip.js" -Wait 8s -After 3s -TimeoutSec 120
"exit=$($r.ExitCode) 用时 $($r.Seconds)s"
($r.Stdout + "`n" + $r.Stderr) -split "`n" | Where-Object { $_ -match 'FAIL|\[push\]|reading|fault' } | Where-Object { $_ -notmatch 'shotjs 返回' } | ForEach-Object { "  $_" }
$res = $r.Results | Select-Object -Last 1
if ($res.error) { "脚本错误：$($res.error)" }
$res.value | Select-Object ok, eventCounts, faults, commands, steps | ConvertTo-Json -Depth 8
"截图：$($r.Png)"
Wait-SandboxExit $sb -TimeoutSec 20 | Out-Null
