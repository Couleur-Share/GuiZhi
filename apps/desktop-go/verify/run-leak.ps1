# 长时间运行内存与泄漏（离屏、隔离沙箱）。
# 每 5 秒采样 GuiZhi.exe 及其全部后代进程（WebView2）的工作集与私有字节，页面内每 5 轮记录 JS 堆与 DOM 节点数。
# 判据：后三分之一相对前三分之一的增长；残留 iframe / 视图宿主应为 0；无 fault。
param([int]$Cycles = 60, [string]$Db = (Join-Path $env:TEMP 'guizhi-golden\knowledge.db'))
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\lib.ps1"
$repo = (Resolve-Path "$PSScriptRoot\..\..\..").Path
$sb = New-Sandbox -Name 'leak'
Remove-Item "$($sb.Root)\ud" -Recurse -Force
Push-Location $repo
try { pnpm dlx tsx apps\desktop-go\verify\make-reading-fixture.mts $Db "$($sb.Root)\ud" 2>&1 | Out-Null } finally { Pop-Location }
$js = Join-Path $sb.Root 'leak-cycle.js'
(Get-Content "$PSScriptRoot\leak-cycle.js" -Raw -Encoding UTF8).Replace('/*CYCLES*/ 60', "$Cycles") | Set-Content $js -Encoding UTF8

$script:rows = New-Object System.Collections.Generic.List[object]
$t0 = Get-Date
$tick = {
  param($rootPid)
  # 进程树：根 + 全部后代（按父子关系，不按名称）
  $all = Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId, Name
  $ids = New-Object System.Collections.Generic.HashSet[int]; [void]$ids.Add($rootPid)
  do { $n = $ids.Count; foreach ($p in $all) { if ($ids.Contains([int]$p.ParentProcessId)) { [void]$ids.Add([int]$p.ProcessId) } } } while ($ids.Count -ne $n)
  $ws = 0; $priv = 0; $cnt = 0
  foreach ($id in $ids) { $pr = Get-Process -Id $id -ErrorAction SilentlyContinue; if ($pr) { $ws += $pr.WorkingSet64; $priv += $pr.PrivateMemorySize64; $cnt++ } }
  $script:rows.Add([pscustomobject]@{ T = [int]((Get-Date) - $t0).TotalSeconds; Procs = $cnt; WorkingSetMB = [math]::Round($ws / 1MB, 0); PrivateMB = [math]::Round($priv / 1MB, 0) })
}
$r = Invoke-Shot -Sandbox $sb -ScriptFile $js -Wait 10s -After 2s -TimeoutSec (60 + $Cycles * 4) -OnTick $tick -TickMs 5000
"exit=$($r.ExitCode) 用时 $($r.Seconds)s 超时=$($r.TimedOut)"
$res = $r.Results | Select-Object -Last 1
if ($res.error) { "脚本错误：$($res.error)" }
"--- 进程树采样（每 5s；节选）---"
$rows | Where-Object { $_.T % 15 -lt 5 } | Format-Table -AutoSize | Out-String -Width 120
$third = [math]::Max(1, [int]($rows.Count / 3))
$early = $rows | Select-Object -Skip 1 -First $third; $late = $rows | Select-Object -Last $third
$m = { param($a, $k) ($a | Measure-Object $k -Average).Average }
"工作集：前三分之一均值 {0:N0} MB → 后三分之一均值 {1:N0} MB" -f (& $m $early 'WorkingSetMB'), (& $m $late 'WorkingSetMB')
"私有字节：前三分之一均值 {0:N0} MB → 后三分之一均值 {1:N0} MB" -f (& $m $early 'PrivateMB'), (& $m $late 'PrivateMB')
"峰值工作集：{0} MB；进程数 {1}-{2}" -f ($rows | Measure-Object WorkingSetMB -Maximum).Maximum, ($rows | Measure-Object Procs -Minimum).Minimum, ($rows | Measure-Object Procs -Maximum).Maximum
$res.value | Select-Object cycles, failures, seconds, faultCount, faults, trend, residual | ConvertTo-Json -Depth 5
Wait-SandboxExit $sb -TimeoutSec 30 | Out-Null
