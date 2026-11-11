# 采样一个进程树的内存：输入根进程 PID，输出 JSON（进程数、工作集合计、私有工作集合计、各进程明细）。
# 私有工作集是任务管理器「内存」列的口径，不会把 Chromium 多进程间共享的页重复计算。
param([Parameter(Mandatory)][int]$RootPid)
if ($RootPid -le 4) { throw "根进程 PID 无效：$RootPid" }
$parentOf = @{}
$nameOf = @{}
foreach ($q in (Get-CimInstance Win32_Process)) {
  $parentOf[[int]$q.ProcessId] = [int]$q.ParentProcessId
  $nameOf[[int]$q.ProcessId] = $q.Name
}
$ids = [System.Collections.Generic.HashSet[int]]::new()
[void]$ids.Add($RootPid)
do {
  $before = $ids.Count
  foreach ($k in @($parentOf.Keys)) {
    if ($k -ne 0 -and $ids.Contains($parentOf[$k])) { [void]$ids.Add($k) }
  }
} while ($ids.Count -ne $before)
$rows = @()
foreach ($r in (Get-CimInstance Win32_PerfFormattedData_PerfProc_Process)) {
  $id = [int]$r.IDProcess
  if ($id -ne 0 -and $ids.Contains($id)) {
    $rows += [pscustomobject]@{ pid = $id; name = $nameOf[$id]; wsMB = [math]::Round($r.WorkingSet / 1MB, 1); privMB = [math]::Round($r.WorkingSetPrivate / 1MB, 1) }
  }
}
[pscustomobject]@{
  procs = $rows.Count
  wsMB = [math]::Round(($rows | Measure-Object wsMB -Sum).Sum, 1)
  privMB = [math]::Round(($rows | Measure-Object privMB -Sum).Sum, 1)
  detail = $rows
} | ConvertTo-Json -Depth 4 -Compress
