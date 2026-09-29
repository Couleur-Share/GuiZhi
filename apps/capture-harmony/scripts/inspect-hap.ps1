# 解包检查 HAP：包名、版本、Ability/扩展、权限，并可断言资源索引里应出现/不应出现的文字。
# 只读；不安装、不改动 HAP。用法：inspect-hap.ps1 -Hap <路径> [-ExpectText 归知收集] [-ForbidText Dev]
# 扩展断言：-ExpectExtension <名称> 要求存在；-ForbidExtensions 要求不得注册任何扩展（正式包）。
# 代码断言：-ForbidCode <标识符> 要求编译产物 ets/modules.abc 里不出现该标识符（确认 spike 代码没有进入正式包）。
# 多个值请在同一会话里用 & 调用并以逗号分隔；经 powershell -File 传数组会被拆坏。
param(
  [Parameter(Mandatory = $true)][string]$Hap,
  [string[]]$ExpectText = @(),
  [string[]]$ForbidText = @(),
  [string[]]$ExpectExtension = @(),
  [string[]]$ForbidCode = @(),
  [switch]$ForbidExtensions,
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
  # 注意：$null | ForEach-Object 仍会执行一次，空列表必须用 foreach 展开，否则会得到一个空元素。
  $abilityItems = @($module.module.abilities | Where-Object { $_ })
  $extensionItems = @($module.module.extensionAbilities | Where-Object { $_ })
  $abilities = @(foreach ($a in $abilityItems) { "$($a.name)(label=$($a.label))" })
  $extensions = @(foreach ($e in $extensionItems) { "$($e.name)(type=$($e.type))" })
  $permissions = @(foreach ($p in @($module.module.requestPermissions | Where-Object { $_ })) { $p.name })
  Write-Output "HAP：$path"
  Write-Output "包名：$($app.bundleName)  版本：$($app.versionName) ($($app.versionCode))  调试：$($app.debug)"
  Write-Output "应用标签：$($app.label)"
  Write-Output "Ability：$($abilities -join ', ')"
  Write-Output "扩展：$(if ($extensions.Count) { $extensions -join ', ' } else { '无' })"
  Write-Output "权限：$($permissions -join ', ')"
  if ($ExpectBundle -and $app.bundleName -ne $ExpectBundle) { $failures.Add("包名应为 $ExpectBundle，实际 $($app.bundleName)") }
  if ($ForbidExtensions -and $extensionItems.Count -gt 0) { $failures.Add("不应注册扩展：$($extensions -join ', ')") }
  foreach ($name in $ExpectExtension) {
    if (-not ($extensionItems | Where-Object { $_.name -eq $name })) { $failures.Add("缺少扩展 $name") }
  }
  $pages = ([Text.Encoding]::UTF8.GetString((Read-Entry 'resources/base/profile/main_pages.json')) | ConvertFrom-Json).src
  Write-Output "路由页面：$(@($pages) -join ', ')"
  if ($ForbidCode.Count -gt 0) {
    # 标识符以明文存放在 abc 的字符串表里，按 Latin1 逐字节还原后做子串匹配即可。
    $abc = [Text.Encoding]::GetEncoding('iso-8859-1').GetString((Read-Entry 'ets/modules.abc'))
    foreach ($code in $ForbidCode) {
      $hits = ([regex]::Matches($abc, [regex]::Escape($code))).Count
      Write-Output "编译产物出现「$code」：$hits 次（应为 0）"
      if ($hits -gt 0) { $failures.Add("编译产物不应出现「$code」") }
    }
  }
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
