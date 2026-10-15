; Per-user installer: no admin rights needed. Built by build-all.sh with makensis.
; Defines passed on the command line: VERSION, ARCH, EXE (path to the app), OUT.
Unicode true
!include "MUI2.nsh"

Name "Jira StopWatch NextGen"
OutFile "${OUT}"
InstallDir "$LOCALAPPDATA\Programs\JiraStopWatchNextGen"
RequestExecutionLevel user
SetCompressor /SOLID lzma
!define MUI_ICON "icon.ico"
!define MUI_UNICON "icon.ico"
!define MUI_FINISHPAGE_RUN "$INSTDIR\jirastopwatch-nextgen.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start Jira StopWatch NextGen"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetOutPath "$INSTDIR"
  File "/oname=jirastopwatch-nextgen.exe" "${EXE}"
  File "icon.ico"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  CreateShortcut "$SMPROGRAMS\Jira StopWatch NextGen.lnk" "$INSTDIR\jirastopwatch-nextgen.exe" "" "$INSTDIR\icon.ico"
  CreateShortcut "$DESKTOP\Jira StopWatch NextGen.lnk" "$INSTDIR\jirastopwatch-nextgen.exe" "" "$INSTDIR\icon.ico"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\JiraStopWatchNextGen" "DisplayName" "Jira StopWatch NextGen"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\JiraStopWatchNextGen" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\JiraStopWatchNextGen" "Publisher" "Farhad Rezvani"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\JiraStopWatchNextGen" "DisplayIcon" "$INSTDIR\icon.ico"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\JiraStopWatchNextGen" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\JiraStopWatchNextGen" "NoModify" 1
SectionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\Jira StopWatch NextGen.lnk"
  Delete "$DESKTOP\Jira StopWatch NextGen.lnk"
  Delete "$INSTDIR\jirastopwatch-nextgen.exe"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\JiraStopWatchNextGen"
SectionEnd
