$in = 'C:\Users\WDAGUtilityAccount\Desktop\in'
$out = 'C:\Users\WDAGUtilityAccount\Desktop\out'
Start-Transcript -Path "$out\prep-upgrade.log" -Force | Out-Null
# 1) 预装旧 Electron 版（静默）；放入 22 条合成数据与「真实 Electron 写出的 Local Storage」夹具（中文设置 + 引导已关闭）
$p = Start-Process "$in\Electron-Setup-0.28.0-x64.exe" -ArgumentList '/S' -Wait -PassThru
"electron install exit = $($p.ExitCode)"
New-Item -ItemType Directory -Force "$env:APPDATA\GuiZhi\data" | Out-Null
Copy-Item "$in\golden-knowledge.db" "$env:APPDATA\GuiZhi\data\knowledge.db" -Force
Copy-Item "$in\legacy-ls\Local Storage" "$env:APPDATA\GuiZhi\Local Storage" -Recurse -Force
"LS files: " + ((Get-ChildItem "$env:APPDATA\GuiZhi\Local Storage\leveldb" | ForEach-Object Name) -join ', ')
# 2) 把沙盒镜像里「登记目录与磁盘不一致」的 WebView2 状态修成正常（走真实机器的常见路径，不再需要环境变量绕行）
$k = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$base = 'C:\Program Files (x86)\Microsoft\EdgeWebView\Application'
$real = (Get-ChildItem $base -Directory | Where-Object { Test-Path "$($_.FullName)\msedgewebview2.exe" } | Select-Object -First 1).FullName
Set-ItemProperty -Path $k -Name EBWebView -Value $real
"EBWebView -> $real"
Stop-Transcript | Out-Null
New-Item -ItemType File -Path "$out\PREP2.flag" -Force | Out-Null