# 解包检查 HAP：包名、版本、Ability/扩展、权限，并可断言资源索引里应出现/不应出现的文字。
# 只读；不安装、不改动 HAP。用法：inspect-hap.ps1 -Hap <路径> [-ExpectText 归知收集] [-ForbidText Dev]
param(
  [Parameter(Mandatory = $true)][string]$Hap,
  [string[]]$ExpectText = @(),
  [string[]]$ForbidText = @(),
  [string]$ExpectBundle
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression.FileSystem
$path = (Resolve-Path -LiteralPath $Hap).Path
$zip = [IO.Compression.ZipFile]::OpenRead($path)
$failures = New-Object System.Collections.Generic.List[string]
try {
  function Read-Entry([string]$name) {
    $entry = $zip.GetEntry($name)
    if (-not $entry) { throw "HAP 中缺少 $name" }
    $memory = New-Object IO.MemoryStream
    $stream = $entry.Open(); $stream.CopyTo($memory); $stream.Close()
    return $memory.ToArray()
  }
  $module = [Text.Encoding]::UTF8.GetString((Read-Entry 'module.json')) | ConvertFrom-Json
  $app = $module.app
  $abilities = @($module.module.abilities | ForEach-Object { "$($_.name)(label=$($_.label))" })
  $extensions = @($module.module.extensionAbilities | ForEach-Object { "$($_.name)(type=$($_.type))" })
  $permissions = @($module.module.requestPermissions | ForEach-Object { $_.name })
  Write-Output "HAP：$path"
  Write-Output "包名：$($app.bundleName)  版本：$($app.versionName) ($($app.versionCode))  调试：$($app.debug)"
  Write-Output "应用标签：$($app.label)"
  Write-Output "Ability：$($abilities -join ', ')"
  Write-Output "扩展：$(if ($extensions.Count) { $extensions -join ', ' } else { '无' })"
  Write-Output "权限：$($permissions -join ', ')"
  if ($ExpectBundle -and $app.bundleName -ne $ExpectBundle) { $failures.Add("包名应为 $ExpectBundle，实际 $($app.bundleName)") }
  $index = [Text.Encoding]::UTF8.GetString((Read-Entry 'resources.index'))
  foreach ($text in $ExpectText) {
    $count = ([regex]::Matches($index, [regex]::Escape($text))).Count
    Write-Output "资源索引出现「$text」：$count 次"
    if ($count -eq 0) { $failures.Add("资源索引缺少「$text」") }
  }
  foreach ($text in $ForbidText) {
    $count = ([regex]::Matches($index, [regex]::Escape($text))).Count
    Write-Output "资源索引出现「$text」：$count 次（应为 0）"
    if ($count -gt 0) { $failures.Add("资源索引不应出现「$text」") }
  }
} finally { $zip.Dispose() }
if ($failures.Count -gt 0) { $failures | ForEach-Object { Write-Output "失败：$_" }; exit 1 }
