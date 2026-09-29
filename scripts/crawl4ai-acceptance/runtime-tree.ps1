# 校验已安装的采集运行包与其分发清单逐文件对应；由 electron-guest.ps1 在每次安装候选后调用。
# 覆盖安装不得残留旧版文件（例如已被裁剪掉的依赖）：应用每次启动提取进程前都会做完整性校验，
# 遇到清单外的文件就以“组件包含未登记文件”拒绝，升级用户会因此失去网页采集。
# Verifies the installed crawler runtime matches its manifest file-for-file: no stale or missing files.
function Assert-RuntimeTree([string]$Runtime) {
  $value = Get-Content -LiteralPath (Join-Path $Runtime 'manifest.json') -Raw | ConvertFrom-Json
  $listed = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
  foreach ($entry in $value.files.PSObject.Properties) { [void]$listed.Add($entry.Name) }
  $prefix = $Runtime.TrimEnd('\') + '\'
  $unregistered = New-Object 'System.Collections.Generic.List[string]'
  $present = 0
  foreach ($file in Get-ChildItem -LiteralPath $Runtime -Recurse -File -Force) {
    $relative = $file.FullName.Substring($prefix.Length).Replace('\', '/')
    if ($relative -eq 'manifest.json') { continue }
    $present++
    if (!$listed.Contains($relative)) { [void]$unregistered.Add($relative) }
  }
  if ($unregistered.Count) { throw "Installed runtime has $($unregistered.Count) unregistered file(s), e.g. $($unregistered[0])" }
  if ($present -ne $listed.Count) { throw "Installed runtime has $present file(s) but the manifest lists $($listed.Count)" }
  return $present
}
