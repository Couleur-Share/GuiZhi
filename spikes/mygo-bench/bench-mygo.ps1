# 基准：MyGo 壳层原型（同一份渲染进程构建、同一个数据库副本）冷启动到首屏就绪的耗时与空闲内存。
# 就绪判据与 Electron 版一致：topbar-search 出现，由壳层 -bench 模式检测后打印 READY。
# 用法：powershell -File bench-mygo.ps1 -Exe <壳层.exe> -Dist <dist 目录> -DbFile <knowledge.db 来源> [-N 5] [-IdleSec 10] [-Out 结果.json]
param([Parameter(Mandatory)][string]$Exe, [Parameter(Mandatory)][string]$Dist, [Parameter(Mandatory)][string]$DbFile, [int]$N = 5, [int]$IdleSec = 10, [string]$Out = "$env:TEMP\bench-mygo.json")
$work = Join-Path $env:TEMP ("guizhi-bench-mygo-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force "$work\wv2" | Out-Null
Copy-Item $DbFile "$work\knowledge.db"
$rows = @()
try {
  for ($i = 0; $i -lt $N; $i++) {
    $psi = [System.Diagnostics.ProcessStartInfo]::new($Exe)
    $psi.Arguments = "-dist `"$Dist`" -db `"$work\knowledge.db`" -bench"
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.CreateNoWindow = $true
    $psi.Environment['WEBVIEW2_USER_DATA_FOLDER'] = "$work\wv2"
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $proc = [System.Diagnostics.Process]::Start($psi)
    try {
      $ready = $null
      $task = $proc.StandardOutput.ReadLineAsync()
      while ($sw.Elapsed.TotalSeconds -lt 60) {
        if ($task.Wait(20)) {
          if ($task.Result -eq 'READY') { $ready = $sw.Elapsed.TotalMilliseconds; break }
          $task = $proc.StandardOutput.ReadLineAsync()
        }
        if ($proc.HasExited) { break }
      }
      if ($null -eq $ready) { throw "第 $($i+1) 次没有在 60 秒内就绪" }
      Start-Sleep -Seconds $IdleSec
      $m = & "$PSScriptRoot\tree-mem.ps1" -RootPid $proc.Id | ConvertFrom-Json
      $row = [ordered]@{ trial = $i + 1; profile = $(if ($i -eq 0) { 'cold' } else { 'warm' }); readyMs = [int]$ready; procs = $m.procs; wsMB = $m.wsMB; privMB = $m.privMB }
      $rows += [pscustomobject]$row
      $row | ConvertTo-Json -Compress
    } finally {
      if (-not $proc.HasExited) { & taskkill /PID $proc.Id /T /F *> $null }
    }
  }
} finally {
  Start-Sleep -Seconds 1
  Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}
$rows | ConvertTo-Json | Set-Content $Out -Encoding utf8
