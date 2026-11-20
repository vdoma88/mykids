; Установщик домашнего сервера MyKids для Windows.
;
; Для семьи, у которой сервер — обычный компьютер, а не NAS: ни Docker, ни
; git, ни терминала. Внутри Node.js, PostgreSQL и сам MyKids; служба
; mykids-server поднимает их при включении компьютера, делает резервные
; копии и перезапускает упавшее. Управление — окном «MyKids Сервер».
;
; Сборка: deploy/server-windows/build.sh. Пути к файлам — относительно
; этого каталога: makensis переходит в него.

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
RequestExecutionLevel admin

!ifndef VERSION
  !error "задайте версию: -DVERSION=0.9.1"
!endif
!ifndef STAGE
  !error "задайте каталог комплекта: -DSTAGE=build/server-win"
!endif

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\MyKidsServer"
!define SERVER "mykids-server.exe"
!define MENU "$SMPROGRAMS\MyKids Сервер"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "WordFunc.nsh"
!include "TextFunc.nsh"
!include "x64.nsh"

Name "MyKids Сервер"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\MyKids Server"
BrandingText "MyKids Сервер ${VERSION}"
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1049 "ProductName" "MyKids Сервер"
VIAddVersionKey /LANG=1049 "FileDescription" "Установка домашнего сервера MyKids"
VIAddVersionKey /LANG=1049 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1049 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=1049 "CompanyName" "MyKids"
VIAddVersionKey /LANG=1049 "LegalCopyright" "MyKids"

!define MUI_ICON "..\installer\mykids.ico"
!define MUI_UNICON "..\installer\mykids.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "..\installer\welcome.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "..\installer\welcome.bmp"
!define MUI_HEADERIMAGE
!define MUI_HEADERIMAGE_RIGHT
!define MUI_HEADERIMAGE_BITMAP "..\installer\header.bmp"
!define MUI_HEADERIMAGE_UNBITMAP "..\installer\header.bmp"
!define MUI_ABORTWARNING
!define MUI_UNABORTWARNING

Var Data       ; каталог данных
Var Installed  ; версия, которая уже стоит
Var Welcome
Var Report     ; последний отчёт mykids-server.exe
Var Addresses  ; адреса MyKids для последней страницы
Var Port

!define MUI_WELCOMEPAGE_TITLE "MyKids Сервер ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "$Welcome"
!insertmacro MUI_PAGE_WELCOME

!define MUI_PAGE_CUSTOMFUNCTION_PRE DirectoryPre
!insertmacro MUI_PAGE_DIRECTORY

!insertmacro MUI_PAGE_INSTFILES

!define MUI_FINISHPAGE_TITLE "Сервер MyKids работает"
!define MUI_FINISHPAGE_TEXT "$Addresses"
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Открыть админку и завести семью"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenAdmin
!define MUI_FINISHPAGE_SHOWREADME
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Открыть окно управления сервером"
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION OpenPanel
!define MUI_FINISHPAGE_NOAUTOCLOSE
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_COMPONENTS
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "Russian"

; --- Отчёт mykids-server.exe (UTF-16LE, см. setup.UTF16) ---------------------

!macro DefineReport UN
Function ${UN}ReadReport
  Exch $0
  Push $1
  Push $2
  StrCpy $Report ""
  ClearErrors
  FileOpen $1 $0 r
  ${IfNot} ${Errors}
    ${Do}
      FileReadUTF16LE $1 $2
      ${If} ${Errors}
        ${Break}
      ${EndIf}
      StrCpy $Report "$Report$2"
      ${${UN}TrimNewLines} $2 $2
      DetailPrint "    $2"
    ${Loop}
    FileClose $1
  ${EndIf}
  Delete $0
  Pop $2
  Pop $1
  Pop $0
FunctionEnd
!macroend
!insertmacro DefineReport ""
!insertmacro DefineReport "un."

; Вызов mykids-server.exe: код — в $0, отчёт — в журнале и в $Report.
!macro Server ARGS
  ExecWait '"$INSTDIR\${SERVER}" -home "$INSTDIR\." -data "$Data" -out "$PLUGINSDIR\report.txt" ${ARGS}' $0
  Push "$PLUGINSDIR\report.txt"
  !ifdef __UNINSTALL__
    Call un.ReadReport
  !else
    Call ReadReport
  !endif
!macroend

; --- Запуск -------------------------------------------------------------------

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "MyKids Сервер работает только в 64-разрядной Windows." /SD IDOK
    SetErrorLevel 2
    Abort
  ${EndIf}
  SetRegView 64
  SetShellVarContext all
  StrCpy $Data "$APPDATA\MyKids Server"

  ReadRegStr $Installed HKLM "${UNINST_KEY}" "DisplayVersion"
  ReadRegStr $0 HKLM "${UNINST_KEY}" "InstallLocation"
  ${If} $0 != ""
    StrCpy $INSTDIR $0
  ${EndIf}
  ${If} $Installed != ""
    ${VersionCompare} $Installed "${VERSION}" $1
    ${If} $1 == 1
      MessageBox MB_ICONEXCLAMATION "Здесь уже стоит MyKids Сервер $Installed — новее этого установщика (${VERSION})." /SD IDOK
      SetErrorLevel 4
      Abort
    ${EndIf}
    StrCpy $Welcome "Здесь стоит MyKids Сервер $Installed. Мастер обновит его до ${VERSION}.$\r$\n$\r$\nБаза, семья, учёт и резервные копии сохранятся. Перед обновлением служба остановится на пару минут: в это время админка и задания ребёнка недоступны, а компьютер ребёнка копит учёт у себя и отправит его позже.$\r$\n$\r$\nНажмите «Далее»."
  ${Else}
    StrCpy $Welcome "Мастер поставит домашний сервер MyKids на этот компьютер.$\r$\n$\r$\nНа нём будут жить админка родителя, задания ребёнка и учёт экранного времени — всё дома, без облака. Компьютер ребёнка будет связываться с ним по домашней сети.$\r$\n$\r$\nЭтот компьютер должен быть включён, когда ребёнок пользуется своим. Если он выключен, компьютер ребёнка копит учёт у себя и отправит его позже.$\r$\n$\r$\nНичего настраивать не нужно: база заведётся сама."
  ${EndIf}
FunctionEnd

Function DirectoryPre
  ${If} $Installed != ""
    Abort
  ${EndIf}
FunctionEnd

; --- Установка ----------------------------------------------------------------

Section "MyKids Сервер" SecMain
  SectionIn RO
  InitPluginsDir

  ${If} $Installed != ""
  ${AndIf} ${FileExists} "$INSTDIR\${SERVER}"
    DetailPrint "Остановка сервера"
    !insertmacro Server "service stop"
  ${EndIf}

  DetailPrint "Копирование программ"
  ; Прежний комплект API убираем целиком: файлы, которых в новой версии нет,
  ; иначе остались бы лежать — например, старая сборка страниц.
  RMDir /r "$INSTDIR\app"
  SetOutPath "$INSTDIR"
  File /r "${STAGE}\*"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "MyKids Сервер"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "MyKids"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\mykids.ico"
  WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegStr HKLM "${UNINST_KEY}" "URLInfoAbout" "https://github.com/vdoma88/mykids"
  WriteRegStr HKLM "${UNINST_KEY}" "Comments" "Домашний сервер MyKids: админка родителя и задания ребёнка"
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize" "$0"

  DetailPrint "Каталог данных"
  !insertmacro Server "protect"
  ${If} $0 != 0
    MessageBox MB_ICONSTOP "Не удалось завести каталог данных.$\r$\n$\r$\n$Report" /SD IDOK
    SetErrorLevel 1
    Abort "Установка не завершена"
  ${EndIf}

  DetailPrint "Брандмауэр: доступ из домашней сети"
  !insertmacro Server "firewall add"

  DetailPrint "Служба"
  !insertmacro Server "service install"
  ${If} $0 != 0
    MessageBox MB_ICONSTOP "Службу зарегистрировать не удалось.$\r$\n$\r$\n$Report" /SD IDOK
    SetErrorLevel 1
    Abort "Установка не завершена"
  ${EndIf}
  !insertmacro Server "service start"

  DetailPrint "Запуск: при первом запуске заводится база, это до минуты"
  !insertmacro Server "-timeout 4m wait"
  ${If} $0 != 0
    MessageBox MB_ICONSTOP "Сервер установлен, но не ответил.$\r$\n$\r$\n$Report$\r$\nОткройте «MyKids Сервер» в меню «Пуск» — там журналы и кнопка «Перезапустить»." /SD IDOK
    SetErrorLevel 1
    Abort "Сервер не ответил"
  ${EndIf}
  StrCpy $Addresses "$Report$\r$\nЭтот адрес «с других устройств дома» вписывают в установщик MyKids на компьютере ребёнка. Его же всегда покажет «MyKids Сервер» в меню «Пуск»."

  !insertmacro Server "port"
  ${TrimNewLines} $Report $Port

  DetailPrint "Ярлыки"
  CreateDirectory "${MENU}"
  CreateShortcut "${MENU}\MyKids Сервер.lnk" "$INSTDIR\${SERVER}" "" "$INSTDIR\mykids.ico" 0 SW_SHOWNORMAL "" "Состояние сервера, адреса, резервные копии"
  CreateShortcut "${MENU}\Удалить MyKids Сервер.lnk" "$INSTDIR\uninstall.exe" "" "$INSTDIR\mykids.ico"
  WriteINIStr "${MENU}\Админка MyKids.url" "InternetShortcut" "URL" "http://localhost:$Port"
  WriteINIStr "${MENU}\Админка MyKids.url" "InternetShortcut" "IconFile" "$INSTDIR\mykids.ico"
  WriteINIStr "${MENU}\Админка MyKids.url" "InternetShortcut" "IconIndex" "0"
  CreateShortcut "$DESKTOP\MyKids Сервер.lnk" "$INSTDIR\${SERVER}" "" "$INSTDIR\mykids.ico"
SectionEnd

Function OpenAdmin
  ExecShell "open" "http://localhost:$Port"
FunctionEnd

Function OpenPanel
  Exec '"$INSTDIR\${SERVER}"'
FunctionEnd

; --- Удаление -----------------------------------------------------------------

Section "un.Программы и служба" SecUnMain
  SectionIn RO
  InitPluginsDir
  ${If} ${FileExists} "$INSTDIR\${SERVER}"
    DetailPrint "Остановка сервера"
    !insertmacro Server "service stop"
    !insertmacro Server "service uninstall"
    ${If} $0 != 0
      MessageBox MB_ICONSTOP "Службу сервера снять не удалось — удаление прервано.$\r$\n$\r$\n$Report" /SD IDOK
      SetErrorLevel 1
      Abort "Удаление прервано"
    ${EndIf}
    !insertmacro Server "firewall remove"
  ${EndIf}

  ; Только своё: каталог программы мог оказаться где угодно, и RMDir /r по
  ; нему целиком снёс бы и чужое, если родитель выбрал общую папку.
  RMDir /r "$INSTDIR\app"
  RMDir /r "$INSTDIR\node"
  RMDir /r "$INSTDIR\pgsql"
  RMDir /r "$INSTDIR\licenses"
  Delete "$INSTDIR\${SERVER}"
  Delete "$INSTDIR\mykids.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "${MENU}\MyKids Сервер.lnk"
  Delete "${MENU}\Удалить MyKids Сервер.lnk"
  Delete "${MENU}\Админка MyKids.url"
  RMDir "${MENU}"
  Delete "$DESKTOP\MyKids Сервер.lnk"
  DeleteRegKey HKLM "${UNINST_KEY}"
SectionEnd

; Данные по умолчанию остаются: это вся история семьи. Стереть их можно
; только прямой галочкой.
Section /o "un.База MyKids и резервные копии" SecUnData
  DetailPrint "Удаление данных: $Data"
  RMDir /r "$Data"
SectionEnd

!insertmacro MUI_UNFUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecUnMain} "Служба, программы, ярлыки, правило брандмауэра."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecUnData} "ВСЯ история семьи: дети, правила, учёт, кредиты, резервные копии. Без галочки останется на месте, и новая установка подхватит её."
!insertmacro MUI_UNFUNCTION_DESCRIPTION_END

Function un.onInit
  SetRegView 64
  SetShellVarContext all
  StrCpy $Data "$APPDATA\MyKids Server"
  ${un.GetParameters} $R0
  ClearErrors
  ${un.GetOptions} $R0 "/PURGE" $R1
  ${IfNot} ${Errors}
    SectionSetFlags ${SecUnData} ${SF_SELECTED}
  ${EndIf}
FunctionEnd
