# 采集窗口池长时间 / 大批量压测的外部驱动（隔离、离屏、只访问本机 httptest）。
# 编译 TestCapturePoolSoak 为独立测试可执行文件并启动，按进程父子关系（不按名称）每 10 秒采样它的整棵进程树
# （测试进程 + 采集子进程 + WebView2 子进程）的工作集与私有字节；结束后检查是否有残留进程和临时目录。
#   用法：.\run-pool-soak.ps1 -Tasks 1500 [-Concurrency 4] [-IdleWait 100]
param([int]$Tasks = 1500, [int]$Concurrency = 4, [int]$IdleWait = 100, [int]$TimeoutMin = 40)
$ErrorActionPreference = 'Stop'
$shell = (Resolve-Path "$PSScriptRoot\..").Path
$work = Join-Path $env:TEMP 'guizhi-verify\pool-soak'
if (Test-Path $work) { Remove-Item $work -Recurse -Force }
New-Item -ItemType Directory -Force $work | Out-Null
$exe = Join-Path $work 'soak.test.exe'
Push-Location $shell
try { go test -c -o $exe . 2>&1 | Out-String | Write-Host } finally { Pop-Location }

# 采集子进程会在 %TEMP% 下建 guizhi-capture-*；记下开始前已有的，结束后只比较新增的
$before = @(Get-ChildItem $env:TEMP -Directory -Filter 'guizhi-capture-*' -ErrorAction SilentlyContinue | ForEach-Object Name)
$out = Join-Path $work 'soak-result.json'
$psi = [Diagnostics.ProcessStartInfo]::new($exe)
foreach ($a in @('-test.run', '^TestCapturePoolSoak$', '-test.v', '-test.timeout', "${TimeoutMin}m")) { $psi.ArgumentList.Add($a) }
$psi.WorkingDirectory = $shell
$psi.UseShellExecute = $false; $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true; $psi.CreateNoWindow = $true
$psi.Environment['GUIZHI_SOAK_TASKS'] = "$Tasks"
$psi.Environment['GUIZHI_SOAK_CONCURRENCY'] = "$Concurrency"
$psi.Environment['GUIZHI_SOAK_IDLE_WAIT'] = "$IdleWait"
$psi.Environment['GUIZHI_SOAK_OUT'] = $out
$t0 = Get-Date
$proc = [Diagnostics.Process]::Start($psi)
$outTask = $proc.StandardOutput.ReadToEndAsync(); $errTask = $proc.StandardError.ReadToEndAsync()
$rows = New-Object System.Collections.Generic.List[object]
$deadline = (Get-Date).AddMinutes($TimeoutMin + 2)
while (-not $proc.HasExited -and (Get-Date) -lt $deadline) {
  $all = Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId
  $ids = New-Object System.Collections.Generic.HashSet[int]; [void]$ids.Add($proc.Id)
  do { $n = $ids.Count; foreach ($p in $all) { if ($ids.Contains([int]$p.ParentProcessId)) { [void]$ids.Add([int]$p.ProcessId) } } } while ($ids.Count -ne $n)
  $ws = 0; $priv = 0; $cnt = 0
  foreach ($id in $ids) { $pr = Get-Process -Id $id -ErrorAction SilentlyContinue; if ($pr) { $ws += $pr.WorkingSet64; $priv += $pr.PrivateMemorySize64; $cnt++ } }
  $rows.Add([pscustomobject]@{ T = [int]((Get-Date) - $t0).TotalSeconds; Procs = $cnt; WorkingSetMB = [math]::Round($ws / 1MB, 0); PrivateMB = [math]::Round($priv / 1MB, 0) })
  $null = $proc.WaitForExit(10000)
}
if (-not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue; "超时：已按精确 PID 结束测试进程 $($proc.Id)" }
$rows | Export-Csv (Join-Path $work 'samples.csv') -NoTypeInformation
Set-Content (Join-Path $work 'test-stdout.log') $outTask.Result; Set-Content (Join-Path $work 'test-stderr.log') $errTask.Result
"退出码 $($proc.ExitCode) 用时 $([int]((Get-Date) - $t0).TotalSeconds)s 采样 $($rows.Count) 次"
if (Test-Path $out) { Get-Content $out -Raw -Encoding UTF8 }

# ---- 内存曲线（前三分之一 / 后三分之一；忽略首个采样）----
$third = [math]::Max(1, [int]($rows.Count / 3))
$early = $rows | Select-Object -Skip 1 -First $third; $late = $rows | Select-Object -Skip ($rows.Count - $third)
$m = { param($a, $k) ($a | Measure-Object $k -Average).Average }
"工作集：前三分之一均值 {0:N0} MB → 后三分之一均值 {1:N0} MB；峰值 {2} MB；进程数 {3}-{4}" -f (& $m $early 'WorkingSetMB'), (& $m $late 'WorkingSetMB'), ($rows | Measure-Object WorkingSetMB -Maximum).Maximum, ($rows | Measure-Object Procs -Minimum).Minimum, ($rows | Measure-Object Procs -Maximum).Maximum
"私有字节：前三分之一均值 {0:N0} MB → 后三分之一均值 {1:N0} MB" -f (& $m $early 'PrivateMB'), (& $m $late 'PrivateMB')
"--- 每分钟节选 ---"
$rows | Where-Object { $_.T % 60 -lt 10 } | Format-Table -AutoSize | Out-String -Width 120

# ---- 残留检查 ----
Start-Sleep -Seconds 3
$after = @(Get-ChildItem $env:TEMP -Directory -Filter 'guizhi-capture-*' -ErrorAction SilentlyContinue | ForEach-Object Name)
$new = @($after | Where-Object { $before -notcontains $_ })
"残留采集临时目录（本次新增且仍在）：$($new.Count) 个 $($new -join ', ')"
$alive = Get-Process -Id $proc.Id -ErrorAction SilentlyContinue
"测试进程是否仍存活：$([bool]$alive)"
