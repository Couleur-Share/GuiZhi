; MyGo 壳安装器的额外检查（仅 GUIZHI_MYGO_SHELL 构建引入，见 electron-builder.mygo.cjs）。
; 注意：本文件含中文字符串，必须保存为带 BOM 的 UTF-8，NSIS 才会按 Unicode 读取。
; 对话框的跳过规则：静默安装（/S）全部跳过；应用内更新（updater 以 `--updated --force-run` 启动，非静默，对应 quitAndInstall(false, true)）只做 WebView2 检查，
; 不弹「覆盖安装前请备份」——更新前应用已自动留了 pre-update 备份，且此时应用已退出，提示里「打开设置→备份」无从谈起。

!define GUIZHI_WEBVIEW2_URL "https://developer.microsoft.com/microsoft-edge/webview2/"

; WebView2 运行时完整性检查，判据与 MyGo 运行时（mygo internal/webview2 的 Find/ClientDLL）一致：
; 读 EdgeUpdate\ClientState\{通道GUID} 的 EBWebView 值得到运行时目录，目录里必须真有
; msedgewebview2.exe 和 EBWebView\x64\EmbeddedBrowserWebView.dll。只看 Clients\{GUID}\pv 不够：
; 实测 Windows 沙盒里 pv 与 ClientState 都登记 157.0.4341.42，磁盘上却只有 157.0.4341.46，
; 安装器放行后应用启动就会因「WebView2 缺失或不完整」失败。
; 结果写入 $R4（1 = 找到完整运行时），$R6（1 = 至少有一处登记，哪怕文件不全）。
!macro guizhiProbeWebView2One root path
  ReadRegStr $R5 ${root} "${path}" "EBWebView"
  ${if} $R5 != ""
    StrCpy $R6 1
    ${if} ${FileExists} "$R5\msedgewebview2.exe"
    ${andIf} ${FileExists} "$R5\EBWebView\x64\EmbeddedBrowserWebView.dll"
      StrCpy $R4 1
    ${endif}
  ${endif}
!macroend

!macro guizhiProbeWebView2Channel guid
  !insertmacro guizhiProbeWebView2One HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\ClientState\${guid}"
  !insertmacro guizhiProbeWebView2One HKLM "SOFTWARE\Microsoft\EdgeUpdate\ClientState\${guid}"
  !insertmacro guizhiProbeWebView2One HKCU "Software\Microsoft\EdgeUpdate\ClientState\${guid}"
!macroend

; 由 installer.nsh 的 customInit 末尾调用。使用寄存器 $R4-$R6；$R0/$R1 是 customInit 已读出的既有安装位置。
!macro guizhiMygoPreInstallChecks
  ${ifNot} ${Silent}
    ; 1) WebView2 运行时：稳定版、Beta、Dev、Canary 任一通道完整即可（与 MyGo 运行时的查找顺序一致）；Windows 11 自带
    StrCpy $R4 0
    StrCpy $R6 0
    !insertmacro guizhiProbeWebView2Channel "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
    !insertmacro guizhiProbeWebView2Channel "{2CD8A007-E189-409D-A2C8-9AF4EF3C72AA}"
    !insertmacro guizhiProbeWebView2Channel "{0D50BFEC-CD6A-4F9A-964C-C7416E3ACB10}"
    !insertmacro guizhiProbeWebView2Channel "{65C35B14-6C1D-4122-AC46-7148CC9D6497}"
    ${if} $R4 != 1
      ; 注意：MessageBox 的 IDNO +2 必须紧跟着要跳过的那一条指令，所以先用变量选文案，不要在 if/else 里各放一个 MessageBox
      ${if} $R6 == 1
        StrCpy $R5 "检测到 Microsoft Edge WebView2 运行时的登记信息，但对应文件缺失或不完整，归知无法启动。"
      ${else}
        StrCpy $R5 "未检测到 Microsoft Edge WebView2 运行时，归知需要它才能运行。"
      ${endif}
      MessageBox MB_YESNO|MB_ICONEXCLAMATION "$R5$\r$\n$\r$\n是否打开微软的下载页面？请安装或修复 WebView2 后，再重新运行本安装程序。" IDNO +2
      ExecShell "open" "${GUIZHI_WEBVIEW2_URL}"
      Abort
    ${endif}

    ; 2) 覆盖安装前提示备份：已登记过安装位置（含旧的 Electron 版）就提醒一次，可取消；应用内更新（--updated）不提示
    ${ifNot} ${isUpdated}
    ${if} $R0 != ""
    ${orIf} $R1 != ""
      MessageBox MB_OKCANCEL|MB_ICONINFORMATION "检测到已安装的归知，本次安装会覆盖它。$\r$\n$\r$\n你的资料保存在 %APPDATA%\GuiZhi，安装与卸载都不会删除。仍建议先在归知里打开「设置」→「备份」创建一份备份，再继续安装。$\r$\n$\r$\n现在继续安装吗？" IDOK +2
      Abort
    ${endif}
    ${endif}
  ${endif}
!macroend
