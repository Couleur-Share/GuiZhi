param(
  [string]$ToolsRoot = $env:HARMONY_COMMAND_LINE_TOOLS,
  [switch]$Build
)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$profile = Join-Path $projectRoot 'build-profile.json5'
if (-not (Test-Path -LiteralPath $profile)) {
  Copy-Item -LiteralPath (Join-Path $projectRoot 'build-profile.template.json5') -Destination $profile
}
$hvigor = $null
$ohpm = $null
if ($ToolsRoot) {
  $toolRootPath = (Resolve-Path -LiteralPath $ToolsRoot).Path
  foreach ($relative in @('bin/ohpm.bat', 'ohpm/bin/ohpm.bat', 'tools/ohpm/bin/ohpm.bat')) {
    $candidate = Join-Path $toolRootPath $relative
    if (Test-Path -LiteralPath $candidate) { $ohpm = $candidate; break }
  }
  foreach ($relative in @('bin/hvigorw.bat', 'hvigor/bin/hvigorw.bat', 'tools/hvigor/bin/hvigorw.bat')) {
    $candidate = Join-Path $toolRootPath $relative
    if (Test-Path -LiteralPath $candidate) { $hvigor = $candidate; break }
  }
  $sdk = Join-Path $toolRootPath 'sdk'
  if (Test-Path -LiteralPath $sdk) { $env:DEVECO_SDK_HOME = $sdk }
  foreach ($nodeRelative in @('tool/node', 'node', 'tools/node')) {
    $nodePath = Join-Path $toolRootPath $nodeRelative
    if (Test-Path -LiteralPath (Join-Path $nodePath 'node.exe')) {
      $env:NODE_HOME = $nodePath
      $env:DEVECO_NODE_HOME = $nodePath
      $env:Path = "$nodePath;$env:Path"
      break
    }
  }
  $jbr = Join-Path $toolRootPath 'jbr'
  if (Test-Path -LiteralPath $jbr) { $env:JAVA_HOME = $jbr }
}
if (-not $hvigor) {
  $command = Get-Command hvigorw.bat -ErrorAction SilentlyContinue
  if ($command) { $hvigor = $command.Source }
}
if (-not $hvigor) {
  Write-Output '源码工程已准备好；缺少华为 Hvigor / HarmonyOS SDK，尚不能编译 HAP。'
  Write-Output '请从官方入口获取 DevEco Studio 或 Command Line Tools，然后用 -ToolsRoot 指向安装/解压根目录。'
  Write-Output 'https://developer.huawei.com/consumer/cn/download/command-line-tools-for-hmos'
  exit 2
}
Write-Output "工程：$projectRoot"
Write-Output "构建工具：$hvigor"
if (-not $Build) {
  Write-Output '工具已找到。传入 -Build 可执行未签名 HAP 构建；工具存在不等于 SDK 或签名已验证。'
  exit 0
}
Push-Location -LiteralPath $projectRoot
try {
  if (-not $ohpm) {
    $command = Get-Command ohpm.bat -ErrorAction SilentlyContinue
    if ($command) { $ohpm = $command.Source }
  }
  if (-not $ohpm) { throw '缺少 ohpm，请使用完整的华为命令行工具或 DevEco 工具目录。' }
  & $ohpm install
  if ($LASTEXITCODE -ne 0) { throw "鸿蒙依赖同步未通过，退出码 $LASTEXITCODE" }
  & $hvigor --mode module -p 'product=default' -p 'module=entry@default' -p 'buildMode=debug' assembleHap --no-daemon
  if ($LASTEXITCODE -ne 0) { throw "鸿蒙编译未通过，退出码 $LASTEXITCODE" }
  $outputs = @(Get-ChildItem -LiteralPath (Join-Path $projectRoot 'entry/build') -Filter '*.hap' -Recurse -File)
  if ($outputs.Count -eq 0) { throw '构建命令结束，但未发现 HAP；不能报告编译成功。' }
  $outputs | Select-Object FullName, Length
} finally { Pop-Location }
