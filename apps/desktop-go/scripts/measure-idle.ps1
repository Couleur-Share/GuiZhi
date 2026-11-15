<#
.SYNOPSIS
  测量归知进程树的空闲私有工作集（原生界面迁移的验收口径）。

.DESCRIPTION
  口径固定，才能在同一台机器、同一份库副本、同一个 MyGo 版本上把「React 版」和「原生版」放在一起比：
    - 度量：整棵进程树（含 WebView2 子进程）的私有工作集之和，取 Win32_PerfFormattedData_PerfProc_Process.WorkingSetPrivate。
    - 两个状态：
        visible  窗口显示在库列表首屏
        hidden   窗口收到托盘（对窗口调用 ShowWindow(SW_HIDE)，与应用的 win.Hide() 等价）
    - 遮挡检测必须关掉（-WebViewArgs，默认 --disable-features=CalculateNativeWinOcclusion）：Chromium 会把「被别的窗口盖住」「在屏幕外」的窗口当成已遮挡，
      并据此释放渲染内存。脚本拉起的窗口不会被激活，常被你正在用的窗口盖住，于是同一个版本的读数在 295–344 MB 之间漂移，
      收托盘后有时降 80 MB、有时不降（2026-11-15，React 版 0.29.0，520 条的库，各 5 次中的前 3 次：可见态 343/344/295，收托盘态 265/341/292）。
      关掉后，可见与否只由窗口自身是否显示决定，读数才可复现。对原生版（没有 Chromium）这个参数无影响。
    - 窗口放置（-Placement）只决定可见态采样时窗口的位置，遮挡检测关闭后三者应一致：
        peek      露出主屏右下角一小块（默认，不盖住你的工作）
        screen    整窗放在主屏 (120,80)，会盖住屏幕
        offscreen 保持 -bench 的屏幕外位置
      收托盘前，脚本都会把整窗放到主屏 (120,80) 并停留 -ShowSeconds 秒（默认 10，窗口不激活），再隐藏，贴近「用户用完关窗」的真实转换。
    - 每个状态静置 -SettleSeconds 秒后采样 -SampleCount 次（间隔 -SampleIntervalSeconds 秒），取中位数；整个流程重复 -Runs 次，再取中位数。
    - 每个标签（-Label）只用一次性目录，库从 -DbSource 拷贝；先热身启动一次（建好 WebView2 配置），再正式测量，贴近日常「已有配置」的使用。
    - 只结束自己拉起的进程树（taskkill /PID /T），不按进程名批量结束；不碰用户数据目录；不碰正在运行的用户实例。

.PARAMETER Exe          GuiZhi.exe 路径（不要指向用户正在运行的那份：请用独立 worktree 或副本构建的产物）
.PARAMETER DbSource     作为「同一份库副本」的 knowledge.db（及同名 -wal/.clean 一并拷贝），可由 seed_library_test.go 生成
.PARAMETER Label        结果标签，例如 react-0.8.3；也是一次性目录名
.PARAMETER Dist         渲染产物目录；默认取 Exe 同级的 dist（存在时），原生版没有 dist 可不传
.PARAMETER ExtraArgs    附加到命令行的参数（原生版的专用参数放这里）
.PARAMETER ReadyPattern 标准输出中表示「已出首屏」的正则，默认 ^READY
.PARAMETER OutJson      结果 JSON 路径；默认写到 <TEMP>\guizhi-idle\<Label>.json

.EXAMPLE
  pwsh -NoProfile -File apps/desktop-go/scripts/measure-idle.ps1 `
    -Exe D:\Project\GuiZhi-baseline\apps\desktop-go\out\win-x64\GuiZhi.exe `
    -DbSource D:\Project\GuiZhi-bench\seed\data\knowledge.db -Label react-0.8.3

.EXAMPLE
  # 原生版（feat/native-ui）：-native 与 -bench 同用，起始页首次加载有结果后打印 READY。
  # 注意：pwsh -File 无法给 string[] 参数传多个值（'-native','-native-module','imports' 会变成一整串），
  # 带 -ExtraArgs 时用 -Command 把数组原样交给脚本。
  pwsh -NoProfile -Command "& 'apps/desktop-go/scripts/measure-idle.ps1' -Exe D:\path\GuiZhi.exe -DbSource D:\path\knowledge.db -Label native -ExtraArgs @('-native','-native-module','imports')"
#>
param(
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$DbSource,
  [Parameter(Mandatory)][string]$Label,
  [string]$Dist = '',
  [string[]]$ExtraArgs = @(),
  [string]$ReadyPattern = '^READY',
  [int]$Runs = 5,
  [int]$SettleSeconds = 60,
  [int]$SampleCount = 3,
  [int]$SampleIntervalSeconds = 3,
  [int]$ReadyTimeoutSeconds = 90,
  [ValidateSet('peek', 'screen', 'offscreen')][string]$Placement = 'peek',
  [int]$ShowSeconds = 10,
  [string]$WebViewArgs = '--disable-features=CalculateNativeWinOcclusion',
  [switch]$SkipHidden,
  [switch]$KeepDir,
  [string]$OutJson = ''
)
$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $Exe)) { throw "找不到 Exe：$Exe" }
if (-not (Test-Path -LiteralPath $DbSource)) { throw "找不到 DbSource：$DbSource" }
$Exe = (Resolve-Path -LiteralPath $Exe).Path
if (-not $Dist) {
  $cand = Join-Path (Split-Path $Exe -Parent) 'dist'
  if (Test-Path -LiteralPath $cand) { $Dist = $cand }
}

Add-Type -Namespace GzIdle -Name Win -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc cb, IntPtr l);
public delegate bool EnumProc(IntPtr h, IntPtr l);
[DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
[DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
[DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
[DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
[DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint flags);
[DllImport("user32.dll")] public static extern int GetSystemMetrics(int index);
[StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
'@

# 取某个进程的最大可见顶层窗口（主窗口；托盘消息窗等隐藏窗口被可见性与面积过滤掉）
function Get-MainWindow([int]$ProcId) {
  $best = [IntPtr]::Zero; $bestArea = 0
  $cb = [GzIdle.Win+EnumProc]{
    param($h, $l)
    $wpid = 0
    [void][GzIdle.Win]::GetWindowThreadProcessId($h, [ref]$wpid)
    if ($wpid -eq $ProcId -and [GzIdle.Win]::IsWindowVisible($h)) {
      $r = New-Object GzIdle.Win+RECT
      [void][GzIdle.Win]::GetWindowRect($h, [ref]$r)
      $area = ($r.R - $r.L) * ($r.B - $r.T)
      if ($area -gt $script:bestArea) { $script:bestArea = $area; $script:best = $h }
    }
    return $true
  }
  [void][GzIdle.Win]::EnumWindows($cb, [IntPtr]::Zero)
  if ($script:bestArea -lt 10000) { return [IntPtr]::Zero }
  return $script:best
}

function Get-Tree([int]$Root) {
  $all = Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId
  $ids = [System.Collections.Generic.List[int]]::new(); $ids.Add($Root)
  for ($i = 0; $i -lt $ids.Count; $i++) {
    foreach ($c in ($all | Where-Object ParentProcessId -eq $ids[$i])) { if (-not $ids.Contains([int]$c.ProcessId)) { $ids.Add([int]$c.ProcessId) } }
  }
  return $ids
}

function Get-Sample([int]$Root) {
  $ids = Get-Tree $Root
  $perf = Get-CimInstance Win32_PerfFormattedData_PerfProc_Process | Where-Object { $ids -contains [int]$_.IDProcess }
  $procs = $perf | ForEach-Object {
    [pscustomobject]@{ Name = $_.Name; Pid = [int]$_.IDProcess; PrivateWsMB = [math]::Round($_.WorkingSetPrivate / 1MB, 1); PrivateBytesMB = [math]::Round($_.PrivateBytes / 1MB, 1); Cpu = [int]$_.PercentProcessorTime }
  }
  [pscustomobject]@{
    Procs          = @($procs).Count
    PrivateWsMB    = [math]::Round((($procs | Measure-Object PrivateWsMB -Sum).Sum), 1)
    PrivateBytesMB = [math]::Round((($procs | Measure-Object PrivateBytesMB -Sum).Sum), 1)
    CpuPct         = ($procs | Measure-Object Cpu -Sum).Sum
    Detail         = @($procs | Sort-Object PrivateWsMB -Descending)
  }
}

function Get-Median([double[]]$v) {
  if ($null -eq $v -or $v.Count -eq 0) { return $null } # 不能写 -not $v：单个 0 元素会被当成假
  $s = $v | Sort-Object
  $n = $s.Count
  if ($n % 2) { return [double]$s[($n - 1) / 2] }
  return ([double]$s[$n / 2 - 1] + [double]$s[$n / 2]) / 2
}

# 静置后采样，返回样本中位数对应的样本（以私有工作集排序取中）
function Measure-State([int]$Root, [string]$Name) {
  Start-Sleep -Seconds $SettleSeconds
  $samples = @()
  for ($i = 0; $i -lt $SampleCount; $i++) {
    if ($i -gt 0) { Start-Sleep -Seconds $SampleIntervalSeconds }
    $samples += Get-Sample $Root
  }
  $mid = ($samples | Sort-Object PrivateWsMB)[[math]::Floor(($samples.Count - 1) / 2)]
  Write-Host ("    {0,-7} {1,7:N1} MB  进程 {2}  CPU {3}%  (样本 {4})" -f $Name, $mid.PrivateWsMB, $mid.Procs, $mid.CpuPct, (($samples | ForEach-Object { $_.PrivateWsMB }) -join '/'))
  return $mid
}

$root = Join-Path $env:TEMP "guizhi-idle\$Label"
if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
$ud = Join-Path $root 'ud'
$dataDir = Join-Path $ud 'data'
New-Item -ItemType Directory -Force $dataDir | Out-Null
$dbSrcDir = Split-Path (Resolve-Path -LiteralPath $DbSource).Path -Parent
$dbName = Split-Path $DbSource -Leaf
foreach ($f in Get-ChildItem -LiteralPath $dbSrcDir -File | Where-Object { $_.Name -like "$dbName*" }) {
  Copy-Item -LiteralPath $f.FullName -Destination (Join-Path $dataDir ($f.Name -replace [regex]::Escape($dbName), 'knowledge.db'))
}
$db = Join-Path $dataDir 'knowledge.db'

function Invoke-OneRun([bool]$Warmup) {
  $stdout = Join-Path $root ("stdout-{0}.txt" -f [guid]::NewGuid().ToString('N').Substring(0, 8))
  $argv = @()
  if ($Dist) { $argv += @('-dist', $Dist) }
  $argv += @('-userdata', $ud, '-db', $db, '-bench') + $ExtraArgs
  # 不能加 -WindowStyle Hidden：GUI 程序首次 ShowWindow 会被启动信息里的 SW_HIDE 覆盖，窗口就永远不可见了
  $prevArgs = $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS
  if ($WebViewArgs) { $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = $WebViewArgs }
  try { $p = Start-Process -FilePath $Exe -ArgumentList $argv -PassThru -RedirectStandardOutput $stdout }
  finally { if ($null -eq $prevArgs) { Remove-Item Env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS -ErrorAction SilentlyContinue } else { $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = $prevArgs } }
  $result = $null
  try {
    $deadline = (Get-Date).AddSeconds($ReadyTimeoutSeconds)
    $ready = $false
    while ((Get-Date) -lt $deadline) {
      if ($p.HasExited) { throw "进程在就绪前退出，退出码 $($p.ExitCode)" }
      if ((Test-Path -LiteralPath $stdout) -and (Select-String -LiteralPath $stdout -Pattern $ReadyPattern -Quiet)) { $ready = $true; break }
      Start-Sleep -Milliseconds 200
    }
    if (-not $ready) { throw "超过 $ReadyTimeoutSeconds 秒未见就绪标记 /$ReadyPattern/" }
    if ($Warmup) {
      Start-Sleep -Seconds 8
      if ($WebViewArgs) { # 验证参数真的传进了 WebView2 浏览器进程，而不是被应用自己的参数覆盖
        $ids = Get-Tree $p.Id
        $cmds = Get-CimInstance Win32_Process | Where-Object { $ids -contains [int]$_.ProcessId -and $_.Name -eq 'msedgewebview2.exe' } | ForEach-Object { $_.CommandLine }
        $flag = ($WebViewArgs -split '\s+' | Select-Object -First 1)
        if (@($cmds | Where-Object { $_ -like "*$flag*" }).Count -gt 0) { Write-Host "   WebView2 参数已生效：$flag" }
        elseif (@($cmds).Count -gt 0) { throw "WebView2 浏览器进程的命令行里没有 $flag（可能被应用自己的参数覆盖），读数会受遮挡检测影响" }
      }
      return $null
    }

    $hwnd = Get-MainWindow $p.Id
    if ($hwnd -eq [IntPtr]::Zero) { throw '找不到主窗口句柄' }
    switch ($Placement) { # SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE = 0x15：只挪位置，不改大小与层级，不激活
      'peek' { [void][GzIdle.Win]::SetWindowPos($hwnd, [IntPtr]::Zero, [GzIdle.Win]::GetSystemMetrics(0) - 60, [GzIdle.Win]::GetSystemMetrics(1) - 60, 0, 0, 0x15) }
      'screen' { [void][GzIdle.Win]::SetWindowPos($hwnd, [IntPtr]::Zero, 120, 80, 0, 0, 0x15) }
    }
    Start-Sleep -Seconds 1 # 让 compositor 先处理一次可见性变化
    $visible = Measure-State $p.Id 'visible'
    $hidden = $null
    if (-not $SkipHidden) {
      if ($Placement -ne 'screen') { # 先让整窗真正可见一会儿，再收托盘，才与「用户用完关窗」的真实转换一致
        [void][GzIdle.Win]::SetWindowPos($hwnd, [IntPtr]::Zero, 120, 80, 0, 0, 0x15)
      }
      Start-Sleep -Seconds $ShowSeconds
      [void][GzIdle.Win]::ShowWindow($hwnd, 0) # SW_HIDE：等价于收到托盘
      $hidden = Measure-State $p.Id 'hidden'
    }
    $result = [pscustomobject]@{ visible = $visible; hidden = $hidden }
  }
  finally {
    if (-not $p.HasExited) { & taskkill.exe /PID $p.Id /T /F *> $null } # 只结束自己拉起的进程树
    Start-Sleep -Seconds 2
  }
  return $result
}

Write-Host "== $Label：$Exe"
Write-Host "   库：$DbSource（$([math]::Round((Get-Item $db).Length / 1MB, 1)) MB）  口径：整棵进程树私有工作集；静置 $SettleSeconds s；$Runs 次取中位数；窗口放置 $Placement"
Write-Host '   热身启动一次…'
[void](Invoke-OneRun $true)

# 注意：PowerShell 变量不区分大小写，这里不能叫 $runs（会与参数 $Runs 撞名）
$results = @()
for ($n = 1; $n -le $Runs; $n++) {
  Write-Host "  第 $n/$Runs 次"
  $results += [pscustomobject](Invoke-OneRun $false) | Add-Member -NotePropertyName n -NotePropertyValue $n -PassThru
}

$summary = [ordered]@{
  visibleMedianMB = Get-Median ($results | ForEach-Object { $_.visible.PrivateWsMB })
  visibleMinMB    = ($results | ForEach-Object { $_.visible.PrivateWsMB } | Measure-Object -Minimum).Minimum
  visibleMaxMB    = ($results | ForEach-Object { $_.visible.PrivateWsMB } | Measure-Object -Maximum).Maximum
  visibleProcs    = Get-Median ($results | ForEach-Object { $_.visible.Procs })
  visibleCpuPct   = Get-Median ($results | ForEach-Object { $_.visible.CpuPct })
}
if (-not $SkipHidden) {
  $summary['hiddenMedianMB'] = Get-Median ($results | ForEach-Object { $_.hidden.PrivateWsMB })
  $summary['hiddenMinMB'] = ($results | ForEach-Object { $_.hidden.PrivateWsMB } | Measure-Object -Minimum).Minimum
  $summary['hiddenMaxMB'] = ($results | ForEach-Object { $_.hidden.PrivateWsMB } | Measure-Object -Maximum).Maximum
  $summary['hiddenProcs'] = Get-Median ($results | ForEach-Object { $_.hidden.Procs })
}
$out = [pscustomobject]@{
  label    = $Label
  exe      = $Exe
  placement = $Placement
  dbMB     = [math]::Round((Get-Item $db).Length / 1MB, 1)
  settleS  = $SettleSeconds
  when     = (Get-Date).ToString('s')
  osBuild  = [System.Environment]::OSVersion.Version.ToString()
  summary  = $summary
  runs     = $results
}
if (-not $OutJson) { $OutJson = Join-Path $env:TEMP "guizhi-idle\$Label.json" }
New-Item -ItemType Directory -Force (Split-Path $OutJson -Parent) | Out-Null
$out | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $OutJson -Encoding utf8
Write-Host '== 汇总（MB，私有工作集）'
([pscustomobject]$summary) | Format-List | Out-String | Write-Host
Write-Host "   结果：$OutJson"
if (-not $KeepDir) { Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue }
