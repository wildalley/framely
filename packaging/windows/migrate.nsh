!include "FileFunc.nsh"
!define FRAMELY_MIGRATION_SCRIPT "${__FILEDIR__}\migrate.ps1"
Var FramelyOldDir

!macro customInit
  ; InstallDirRegKey restores the OLD directory before .onInit runs.
  ; Move only the historical folder name; preserve a user's custom folder.
  ReadRegStr $FramelyOldDir HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.screenpin.desktop" "InstallLocation"
  ${GetFileName} "$INSTDIR" $0
  StrCmp $0 "ScreenPin" 0 framely_init_done
  StrCmp $FramelyOldDir "" 0 +2
  StrCpy $FramelyOldDir "$INSTDIR"
  ${GetParent} "$INSTDIR" $1
  StrCpy $INSTDIR "$1\Framely"
  framely_init_done:
  StrCmp $FramelyOldDir "" framely_close_done
  StrCmp $FramelyOldDir $INSTDIR framely_close_done
  Push "$FramelyOldDir"
  Call CloseApp
  framely_close_done:
!macroend

!macro customInstall
  InitPluginsDir
  SetOutPath "$PLUGINSDIR"
  File /oname=framely-migrate.ps1 "${FRAMELY_MIGRATION_SCRIPT}"
  SetOutPath "$INSTDIR"
  System::Call 'Kernel32::SetEnvironmentVariable(t "FRAMELY_OLD_INSTALL_DIR", t "$FramelyOldDir")'
  System::Call 'Kernel32::SetEnvironmentVariable(t "FRAMELY_INSTALL_DIR", t "$INSTDIR")'
  StrCpy $0 "$WINDIR\Sysnative\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $0 +2
  StrCpy $0 "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe"
  nsExec::ExecToStack '"$0" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\framely-migrate.ps1"'
  Pop $0
  Pop $1
  StrCmp $0 "0" framely_install_done
  MessageBox MB_OK|MB_ICONSTOP "Framely was installed, but migration failed: $1" /SD IDOK
  SetErrorLevel 1
  Quit
  framely_install_done:
!macroend
