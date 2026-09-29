param(
  [string]$ToolsRoot = $env:HARMONY_COMMAND_LINE_TOOLS,
  # default：正式包；dev：真机开发包（独立包名，见 docs/device-loop.md）。
  [ValidateSet('default', 'dev')][string]$Product = 'default',
  # 在系统临时目录的副本中构建：不写入本目录的 entry/build 与 .hvigor，可与 DevEco/HoKit 并行。
  [switch]$Isolated,
  [switch]$Build
)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$repoRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot '../..'))
$profile = Join-Path $projectRoot 'build-profile.json5'
if (-not (Test-Path -LiteralPath $profile)) {
  Copy-Item -LiteralPath (Join-Path $projectRoot 'build-profile.template.json5') -Destination $profile
}
# 产品与 hvigor 目标一一对应；目标定义见 entry/build-profile.json5。
$target = if ($Product -eq 'dev') { 'dev' } else { 'default' }
if ($Product -eq 'dev' -and -not (Select-String -LiteralPath $profile -Pattern '"name"\s*:\s*"dev"' -Quiet)) {
  Write-Output '本机 build-profile.json5 缺少 dev 产品。请对照 build-profile.template.json5 合并 products 与 targets，并保留你已有的 signingConfigs。'
  exit 2
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
Write-Output "工程：$projectRoot（产品 $Product / 目标 $target）"
Write-Output "构建工具：$hvigor"
if (-not $Build) {
  Write-Output '工具已找到。传入 -Build 可执行 HAP 构建；工具存在不等于 SDK 或签名已验证。'
  exit 0
}
# 构建目录：默认原地；-Isolated 时镜像到临时副本（保留其 build/.hvigor/oh_modules 以便增量构建）。
$workRoot = $projectRoot
if ($Isolated) {
  $workRoot = Join-Path ([IO.Path]::GetTempPath()) 'guizhi-harmony-verify'
  New-Item -ItemType Directory -Force -Path $workRoot | Out-Null
  # /XD 同时排除复制与清除：副本里的构建缓存不会被 /MIR 删掉。
  & robocopy $projectRoot $workRoot /MIR /XD build .hvigor oh_modules .idea node_modules /NFL /NDL /NJH /NJS /NP | Out-Null
  if ($LASTEXITCODE -ge 8) { throw "同步隔离副本失败，robocopy 退出码 $LASTEXITCODE" }
  Write-Output "隔离副本：$workRoot"
}
Push-Location -LiteralPath $workRoot
try {
  if (-not $ohpm) {
    $command = Get-Command ohpm.bat -ErrorAction SilentlyContinue
    if ($command) { $ohpm = $command.Source }
  }
  if (-not $ohpm) { throw '缺少 ohpm，请使用完整的华为命令行工具或 DevEco 工具目录。' }
  & $ohpm install
  if ($LASTEXITCODE -ne 0) { throw "鸿蒙依赖同步未通过，退出码 $LASTEXITCODE" }
  & $hvigor --mode module -p "product=$Product" -p "module=entry@$target" -p 'buildMode=debug' assembleHap --no-daemon
  if ($LASTEXITCODE -ne 0) { throw "鸿蒙编译未通过，退出码 $LASTEXITCODE" }
  $outputDir = Join-Path $workRoot "entry/build/$target/outputs/$target"
  $outputs = @()
  if (Test-Path -LiteralPath $outputDir) { $outputs = @(Get-ChildItem -LiteralPath $outputDir -Filter '*.hap' -File) }
  if ($outputs.Count -eq 0) { throw '构建命令结束，但未发现 HAP；不能报告编译成功。' }
  # 统一放到仓库 artifacts/（已被忽略），device.ps1 默认从这里取包。
  $deliver = Join-Path $repoRoot "artifacts/harmony/hap/$Product"
  New-Item -ItemType Directory -Force -Path $deliver | Out-Null
  Get-ChildItem -LiteralPath $deliver -Filter '*.hap' -File | Remove-Item -Force
  $outputs | ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $deliver -Force }
  Get-ChildItem -LiteralPath $deliver -Filter '*.hap' -File | Select-Object FullName, Length
  if (-not ($outputs | Where-Object { $_.Name -notmatch 'unsigned' })) {
    Write-Output '注意：产物均为未签名 HAP，hdc 无法安装。签名步骤见 docs/device-loop.md。'
  }
} finally { Pop-Location }
