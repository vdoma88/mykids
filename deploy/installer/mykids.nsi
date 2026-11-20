; Установщик MyKids для компьютера ребёнка.
;
; Один файл, мастер на русском. Раньше установка была в два приёма: MSI
; раскладывал программы, а привязку и службу делало отдельное окно, которое
; ещё надо было найти в меню «Пуск» и запустить правой кнопкой от имени
; администратора. Шаг, о котором надо помнить, — это шаг, который пропустят:
; программы стоят, служба не зарегистрирована, ребёнок свободен.
;
; Здесь всё за один проход: файлы, привязка, служба, ярлыки, запись в
; «Программах и компонентах». Обновление — тот же файл поверх: привязка
; сохраняется, токен заново не нужен.
;
; Мастер — только оболочка. Что считать ошибкой и что с ней делать, решает
; mykids-setup.exe (agents/windows/cmd/mykids-setup): эти решения покрыты
; тестами, а NSIS проверить нечем, кроме сборки. Поэтому мастер вызывает его
; командами и показывает его отчёт.
;
; Сборка: deploy/build-installer.sh (makensis есть и под Linux).
;
; Без окон, для нескольких машин:
;   MyKids-Setup-X.Y.Z.exe /S /SERVER=http://192.168.1.10:3000 /TOKEN=<токен> /CHILD=<учётная запись>
; Код возврата: 0 — готово, 1 — не прошла проверка или привязка (подробности
; в %TEMP%\mykids-setup.log), 4 — установлена более новая версия.

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
RequestExecutionLevel admin

!ifndef VERSION
  !error "задайте версию: makensis -DVERSION=0.9.0 ..."
!endif
!ifndef DIST
  !error "задайте каталог с .exe: makensis -DDIST=agents/windows/dist ..."
!endif
!ifndef OUTFILE
  !define OUTFILE "${DIST}/MyKids-Setup-${VERSION}.exe"
!endif

!define PRODUCT "MyKids"
; Ключ в «Программах и компонентах». Имя постоянное: по нему новая версия
; находит старую.
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\MyKids"
!define UNINST_ROOT "Software\Microsoft\Windows\CurrentVersion\Uninstall"
!define HELPER "mykids-setup.exe"

!include "MUI2.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "WordFunc.nsh"
!include "TextFunc.nsh"
!include "x64.nsh"

Name "${PRODUCT}"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\MyKids"
BrandingText "MyKids ${VERSION}"
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1049 "ProductName" "MyKids"
VIAddVersionKey /LANG=1049 "FileDescription" "Установка MyKids"
VIAddVersionKey /LANG=1049 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1049 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=1049 "CompanyName" "MyKids"
VIAddVersionKey /LANG=1049 "LegalCopyright" "MyKids"

; --- Оформление ---------------------------------------------------------------

; Пути к картинкам и иконке — относительно этого файла: makensis переходит
; в его каталог.
!define MUI_ICON "mykids.ico"
!define MUI_UNICON "mykids.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_HEADERIMAGE
!define MUI_HEADERIMAGE_RIGHT
!define MUI_HEADERIMAGE_BITMAP "header.bmp"
!define MUI_HEADERIMAGE_UNBITMAP "header.bmp"
!define MUI_ABORTWARNING
!define MUI_UNABORTWARNING

Var Server        ; адрес сервера, как его ввели
Var Token         ; токен; пусто — оставить прежнюю привязку
Var Child         ; учётная запись ребёнка
Var ChildURL      ; адрес заданий — для ярлыка на рабочем столе
Var Bound         ; 1, если компьютер уже привязан
Var BoundServer
Var Installed     ; версия, которая уже стоит; пусто — первая установка
Var Welcome       ; текст первой страницы: для обновления он другой
Var Report        ; текст последнего отчёта mykids-setup.exe

Var hServer
Var hToken
Var hChild

; --- Страницы -----------------------------------------------------------------

!define MUI_WELCOMEPAGE_TITLE "Установка MyKids ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "$Welcome"
!insertmacro MUI_PAGE_WELCOME

Page custom ConnectShow ConnectLeave

!define MUI_PAGE_CUSTOMFUNCTION_PRE DirectoryPre
!insertmacro MUI_PAGE_DIRECTORY

!insertmacro MUI_PAGE_INSTFILES

!define MUI_FINISHPAGE_TITLE "MyKids работает"
!define MUI_FINISHPAGE_TEXT "Служба запущена и будет стартовать вместе с Windows.$\r$\n$\r$\nЧто проверить:$\r$\n$\r$\n1. Войдите в учётную запись ребёнка — в трее появится значок с остатком времени.$\r$\n$\r$\n2. На рабочем столе — ярлык «Задания MyKids»: задания и магазин.$\r$\n$\r$\n3. Пуск → MyKids → «Панель MyKids»: работает ли служба, есть ли связь с сервером, сколько осталось у ребёнка."
!define MUI_FINISHPAGE_RUN "$INSTDIR\mykids-setup.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Открыть панель MyKids"
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!define MUI_FINISHPAGE_LINK "Открыть админку MyKids"
!define MUI_FINISHPAGE_LINK_LOCATION "$Server"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_COMPONENTS
!insertmacro MUI_UNPAGE_INSTFILES
!define MUI_FINISHPAGE_TITLE "MyKids удалён"
!define MUI_FINISHPAGE_TEXT "Служба снята, ограничения на этом компьютере больше не действуют.$\r$\n$\r$\nВ админке устройство пока числится за ребёнком: отвяжите его на странице ребёнка, во вкладке «Устройства»."
!define MUI_UNFINISHPAGE_NOAUTOCLOSE
!insertmacro MUI_UNPAGE_FINISH

!insertmacro MUI_LANGUAGE "Russian"

; --- Отчёт mykids-setup.exe ---------------------------------------------------

; Отчёт читается из файла в UTF-16LE (см. setup.UTF16): целиком в $Report и
; построчно в журнал установки. Функции нужны в двух экземплярах — для
; установщика и для деинсталлятора: NSIS не даёт им общих функций.
!macro DefineReportFunctions UN
Function ${UN}PrintReport
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
!insertmacro DefineReportFunctions ""
!insertmacro DefineReportFunctions "un."

; Вызов mykids-setup.exe. Код возврата — в $0, отчёт — в журнале и в $Report.
!macro Helper EXE ARGS
  ExecWait '"${EXE}" ${ARGS} -out "$PLUGINSDIR\report.txt"' $0
  Push "$PLUGINSDIR\report.txt"
  !ifdef __UNINSTALL__
    Call un.PrintReport
  !else
    Call PrintReport
  !endif
!macroend

; --- Запуск -------------------------------------------------------------------

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "MyKids работает только в 64-разрядной Windows." /SD IDOK
    SetErrorLevel 2
    Abort
  ${EndIf}
  ; Служба и программы — 64-разрядные; без этого записи в реестр и пути
  ; уехали бы в 32-разрядное отражение, и обновление не нашло бы установку.
  SetRegView 64
  SetShellVarContext all

  ; Уже установлено? Тогда ставим туда же и не спрашиваем каталог.
  ReadRegStr $Installed HKLM "${UNINST_KEY}" "DisplayVersion"
  ReadRegStr $0 HKLM "${UNINST_KEY}" "InstallLocation"
  ${If} $0 != ""
    StrCpy $INSTDIR $0
  ${EndIf}

  ; Версии до 0.9 ставились пакетом MSI — своей записи у них нет.
  ${If} $Installed == ""
    Call FindMSI
    StrCpy $Installed $R2
  ${EndIf}
  ; Учётную запись ребёнка помним с прошлой установки: при обновлении
  ; выбирать её заново незачем.
  ReadRegStr $Child HKLM "${UNINST_KEY}" "ChildAccount"

  ${If} $Installed != ""
    ${VersionCompare} $Installed "${VERSION}" $1
    ${If} $1 == 1
      MessageBox MB_ICONEXCLAMATION "Здесь уже стоит MyKids $Installed — новее этого установщика (${VERSION}).$\r$\n$\r$\nЧтобы поставить старую версию, сначала удалите новую." /SD IDOK
      SetErrorLevel 4
      Abort
    ${EndIf}
  ${EndIf}

  InitPluginsDir
  File "/oname=$PLUGINSDIR\${HELPER}" "${DIST}/${HELPER}"

  ; Привязка уже есть — обновление или переустановка. Адрес подставим, токен
  ; спрашивать не будем.
  StrCpy $Bound 0
  ExecWait '"$PLUGINSDIR\${HELPER}" status -out "$PLUGINSDIR\status.txt"' $0
  ${If} $0 == 0
    StrCpy $Bound 1
    ClearErrors
    FileOpen $1 "$PLUGINSDIR\status.txt" r
    ${IfNot} ${Errors}
      FileReadUTF16LE $1 $BoundServer
      ${TrimNewLines} $BoundServer $BoundServer
      FileClose $1
    ${EndIf}
    StrCpy $Server $BoundServer
  ${EndIf}
  Delete "$PLUGINSDIR\status.txt"

  ; Параметры командной строки — для установки без окон.
  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/SERVER=" $R1
  ${IfNot} ${Errors}
    StrCpy $Server $R1
  ${EndIf}
  ClearErrors
  ${GetOptions} $R0 "/TOKEN=" $R1
  ${IfNot} ${Errors}
    StrCpy $Token $R1
  ${EndIf}
  ClearErrors
  ${GetOptions} $R0 "/CHILD=" $R1
  ${IfNot} ${Errors}
    StrCpy $Child $R1
  ${EndIf}

  ${If} $Installed != ""
  ${AndIf} $Bound == 1
    StrCpy $Welcome "На этом компьютере стоит MyKids $Installed. Мастер обновит его до ${VERSION}.$\r$\n$\r$\nПривязка к серверу сохранится — токен вводить заново не нужно. Учёт времени прервётся на минуту, пока служба перезапускается.$\r$\n$\r$\nНажмите «Далее»."
  ${ElseIf} $Installed != ""
    StrCpy $Welcome "На этом компьютере стоит MyKids $Installed, но к серверу он не привязан. Мастер обновит его до ${VERSION} и привяжет.$\r$\n$\r$\nПонадобится токен устройства — кнопка «Выдать токен» на странице ребёнка в админке.$\r$\n$\r$\nНажмите «Далее»."
  ${Else}
    StrCpy $Welcome "Мастер поставит MyKids на этот компьютер — компьютер ребёнка.$\r$\n$\r$\nПонадобятся:$\r$\n$\r$\n•  адрес домашнего сервера MyKids — тот, по которому вы открываете админку;$\r$\n$\r$\n•  токен устройства — кнопка «Выдать токен» на странице ребёнка в админке;$\r$\n$\r$\n•  обычная, не администраторская учётная запись Windows, в которой работает ребёнок.$\r$\n$\r$\nЗапускайте установку из своей учётной записи, а не из учётной записи ребёнка."
  ${EndIf}
FunctionEnd

; --- Подключение к серверу ----------------------------------------------------

Function ConnectShow
  !insertmacro MUI_HEADER_TEXT "Подключение к домашнему серверу" "Куда этот компьютер будет отправлять учёт времени"
  nsDialogs::Create 1018
  Pop $0
  ${If} $0 == error
    Abort
  ${EndIf}

  ${If} $Bound == 1
    ${NSD_CreateLabel} 0 0 100% 24u "Компьютер уже привязан к $BoundServer. Оставьте токен пустым, чтобы сохранить привязку, или вставьте новый, чтобы привязать заново."
  ${Else}
    ${NSD_CreateLabel} 0 0 100% 24u "В админке MyKids откройте страницу ребёнка и нажмите «Выдать токен». Токен показывается один раз — перенесите его сюда сразу."
  ${EndIf}
  Pop $0

  ${NSD_CreateLabel} 0 30u 100% 10u "Адрес сервера — как вы открываете админку, например http://192.168.1.10:3000"
  Pop $0
  ${NSD_CreateText} 0 41u 100% 12u "$Server"
  Pop $hServer

  ${If} $Bound == 1
    ${NSD_CreateLabel} 0 60u 100% 10u "Токен устройства — можно оставить пустым"
  ${Else}
    ${NSD_CreateLabel} 0 60u 100% 10u "Токен устройства"
  ${EndIf}
  Pop $0
  ${NSD_CreatePassword} 0 71u 100% 12u "$Token"
  Pop $hToken

  ${NSD_CreateLabel} 0 90u 100% 10u "Учётная запись ребёнка в Windows — обычная, не администратор"
  Pop $0
  ${NSD_CreateComboBox} 0 101u 100% 80u ""
  Pop $hChild

  ; Подсказка: обычные учётные записи этого компьютера. Ввести имя руками
  ; можно и так — поле редактируемое.
  StrCpy $R2 0
  ExecWait '"$PLUGINSDIR\${HELPER}" accounts -out "$PLUGINSDIR\accounts.txt"' $0
  ClearErrors
  FileOpen $1 "$PLUGINSDIR\accounts.txt" r
  ${IfNot} ${Errors}
    ${Do}
      FileReadUTF16LE $1 $2
      ${If} ${Errors}
        ${Break}
      ${EndIf}
      ${TrimNewLines} $2 $2
      ${If} $2 != ""
        ${NSD_CB_AddString} $hChild $2
        IntOp $R2 $R2 + 1
        StrCpy $R3 $2
      ${EndIf}
    ${Loop}
    FileClose $1
  ${EndIf}
  Delete "$PLUGINSDIR\accounts.txt"

  ${If} $Child != ""
    ${NSD_SetText} $hChild $Child
  ${ElseIf} $R2 == 1
    ; Обычная учётная запись одна — почти наверняка это и есть ребёнок.
    ${NSD_SetText} $hChild $R3
  ${EndIf}

  ${NSD_CreateLabel} 0 120u 100% 20u "Перед продолжением мастер спросит сервер, чей это токен, и проверит права учётной записи. На компьютере пока ничего не меняется."
  Pop $0

  ${If} $Server == ""
    ${NSD_SetFocus} $hServer
  ${ElseIf} $Bound == 1
    ${NSD_SetFocus} $hChild
  ${Else}
    ${NSD_SetFocus} $hToken
  ${EndIf}

  nsDialogs::Show
FunctionEnd

Function ConnectLeave
  ${NSD_GetText} $hServer $Server
  ${NSD_GetText} $hToken $Token
  ${NSD_GetText} $hChild $Child

  Banner::show "Проверяю связь с сервером…"
  ExecWait '"$PLUGINSDIR\${HELPER}" verify -server "$Server" -token "$Token" -child "$Child" -out "$PLUGINSDIR\verify.txt"' $0
  Banner::destroy
  ; В журнал страницы подключения не пишут — отчёт нужен только для окна.
  Push "$PLUGINSDIR\verify.txt"
  Call PrintReport

  ${If} $0 == 0
    MessageBox MB_OKCANCEL|MB_ICONINFORMATION "$Report$\r$\nПродолжить установку?" IDOK confirmed
    Abort
    confirmed:
  ${ElseIf} $0 == 1
    MessageBox MB_ICONEXCLAMATION "$Report"
    Abort
  ${Else}
    MessageBox MB_ICONSTOP "Проверка не выполнилась (код $0).$\r$\n$\r$\n$Report$\r$\nПодробности: %TEMP%\mykids-setup.log"
    Abort
  ${EndIf}
FunctionEnd

; Каталог спрашиваем только при первой установке: обновление идёт туда же,
; куда стояло, иначе служба осталась бы указывать на старое место.
Function DirectoryPre
  ${If} $Installed != ""
    Abort
  ${EndIf}
FunctionEnd

; --- Установка ----------------------------------------------------------------

; Прежние версии ставились пакетом MSI. Его запись в «Программах и
; компонентах» ищем по имени: код продукта у каждой сборки MSI свой.
; Результат: $R1 — код продукта (пусто, если MSI нет), $R2 — его версия.
Function FindMSI
  StrCpy $R0 0
  ${Do}
    EnumRegKey $R1 HKLM "${UNINST_ROOT}" $R0
    ${If} $R1 == ""
      ${Break}
    ${EndIf}
    IntOp $R0 $R0 + 1
    ReadRegStr $R2 HKLM "${UNINST_ROOT}\$R1" "DisplayName"
    ReadRegDWORD $R3 HKLM "${UNINST_ROOT}\$R1" "WindowsInstaller"
    ${If} $R2 == "MyKids"
    ${AndIf} $R3 == 1
      ReadRegStr $R2 HKLM "${UNINST_ROOT}\$R1" "DisplayVersion"
      Return
    ${EndIf}
  ${Loop}
  StrCpy $R1 ""
  StrCpy $R2 ""
FunctionEnd

; Пакет MSI удаляем тихо: две записи «MyKids» — путаница, а удаление старой
; следом снесло бы файлы новой. Служба к этому моменту уже снята, данные в
; ProgramData пакет не трогает.
Function RemoveMSI
  Call FindMSI
  ${If} $R1 != ""
    DetailPrint "Удаление прежней версии $R2, поставленной пакетом MSI"
    ExecWait '"$SYSDIR\msiexec.exe" /x $R1 /qn /norestart' $R4
    ${If} $R4 != 0
      DetailPrint "    msiexec вернул код $R4 — ставлю поверх"
    ${EndIf}
  ${EndIf}
FunctionEnd

Section "MyKids" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"

  ; Без окон страница подключения не показывалась — проверяем здесь.
  ${If} ${Silent}
    DetailPrint "Проверка введённого"
    !insertmacro Helper "$PLUGINSDIR\${HELPER}" 'verify -server "$Server" -token "$Token" -child "$Child"'
    ${If} $0 != 0
      SetErrorLevel 1
      Quit
    ${EndIf}
  ${EndIf}

  DetailPrint "Остановка прежней версии"
  !insertmacro Helper "$PLUGINSDIR\${HELPER}" "stop"
  ${If} $0 != 0
    MessageBox MB_ICONSTOP "Не удалось остановить прежнюю версию MyKids.$\r$\n$\r$\n$Report" /SD IDOK
    SetErrorLevel 1
    Abort "Установка прервана: прежняя версия не остановилась"
  ${EndIf}
  Call RemoveMSI

  DetailPrint "Копирование программ"
  File "${DIST}/mykids-agent.exe"
  File "${DIST}/mykids-check.exe"
  File "${DIST}/mykids-setup.exe"
  File "mykids.ico"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Запись в «Программах и компонентах» — до привязки: если привязка не
  ; удастся, удалить начатое должно быть можно привычным способом.
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "MyKids"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "MyKids"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\mykids.ico"
  WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegStr HKLM "${UNINST_KEY}" "URLInfoAbout" "https://github.com/vdoma88/mykids"
  WriteRegStr HKLM "${UNINST_KEY}" "Comments" "Экранное время за учебные задания"
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize" "$0"

  DetailPrint "Привязка и запуск службы"
  !insertmacro Helper "$INSTDIR\${HELPER}" 'apply -dir "$INSTDIR\." -server "$Server" -token "$Token" -child "$Child"'
  ${If} $0 != 0
    MessageBox MB_ICONSTOP "Программы установлены, но служба не запущена — учёт времени не идёт.$\r$\n$\r$\n$Report$\r$\nИсправьте и запустите этот установщик ещё раз." /SD IDOK
    SetErrorLevel 1
    Abort "Установка не завершена"
  ${EndIf}

  ${If} $Child != ""
    WriteRegStr HKLM "${UNINST_KEY}" "ChildAccount" "$Child"
  ${EndIf}

  ; Адрес заданий — из привязки, а не из поля: при обновлении поле пустое.
  ExecWait '"$INSTDIR\${HELPER}" status -out "$PLUGINSDIR\status.txt"' $0
  ClearErrors
  FileOpen $1 "$PLUGINSDIR\status.txt" r
  ${IfNot} ${Errors}
    FileReadUTF16LE $1 $Server
    ${TrimNewLines} $Server $Server
    FileReadUTF16LE $1 $ChildURL
    ${TrimNewLines} $ChildURL $ChildURL
    FileClose $1
  ${EndIf}
  Delete "$PLUGINSDIR\status.txt"

  DetailPrint "Ярлыки"
  CreateDirectory "$SMPROGRAMS\MyKids"
  ; Панель — главный ярлык для родителя: состояние службы и связи,
  ; перепривязка, перезапуск. Раньше всё это было командой в PowerShell.
  CreateShortcut "$SMPROGRAMS\MyKids\Панель MyKids.lnk" "$INSTDIR\mykids-setup.exe" "" "$INSTDIR\mykids.ico" 0 SW_SHOWNORMAL "" "Состояние MyKids на этом компьютере"
  CreateShortcut "$SMPROGRAMS\MyKids\Проверка MyKids.lnk" "$INSTDIR\mykids-check.exe" "" "$INSTDIR\mykids.ico" 0 SW_SHOWNORMAL "" "Что агент видит на этом компьютере"
  CreateShortcut "$SMPROGRAMS\MyKids\Удалить MyKids.lnk" "$INSTDIR\uninstall.exe" "" "$INSTDIR\mykids.ico"
  ; Задания — главное, что ребёнку нужно от MyKids, и до сих пор адрес он
  ; узнавал только с закрытого экрана. Ярлык на общем рабочем столе виден
  ; в его учётной записи сразу.
  ${If} $ChildURL != ""
    WriteINIStr "$SMPROGRAMS\MyKids\Задания и магазин.url" "InternetShortcut" "URL" "$ChildURL"
    WriteINIStr "$SMPROGRAMS\MyKids\Задания и магазин.url" "InternetShortcut" "IconFile" "$INSTDIR\mykids.ico"
    WriteINIStr "$SMPROGRAMS\MyKids\Задания и магазин.url" "InternetShortcut" "IconIndex" "0"
    WriteINIStr "$DESKTOP\Задания MyKids.url" "InternetShortcut" "URL" "$ChildURL"
    WriteINIStr "$DESKTOP\Задания MyKids.url" "InternetShortcut" "IconFile" "$INSTDIR\mykids.ico"
    WriteINIStr "$DESKTOP\Задания MyKids.url" "InternetShortcut" "IconIndex" "0"
  ${EndIf}

  DetailPrint "Готово"
SectionEnd

; --- Удаление -----------------------------------------------------------------

Section "un.Программы и служба" SecUnMain
  SectionIn RO

  ; Сначала служба и помощник: помощник без службы через минуту закрывает
  ; экран, и после удаления ребёнок остался бы перед закрытым экраном до
  ; перезагрузки.
  ${If} ${FileExists} "$INSTDIR\${HELPER}"
    DetailPrint "Остановка службы"
    InitPluginsDir
    !insertmacro Helper "$INSTDIR\${HELPER}" "stop"
    ${If} $0 != 0
      MessageBox MB_ICONSTOP "Службу MyKids остановить не удалось — удаление прервано, иначе осталась бы служба без программы.$\r$\n$\r$\n$Report" /SD IDOK
      SetErrorLevel 1
      Abort "Удаление прервано"
    ${EndIf}
  ${EndIf}

  Delete "$INSTDIR\mykids-agent.exe"
  Delete "$INSTDIR\mykids-check.exe"
  Delete "$INSTDIR\mykids-setup.exe"
  Delete "$INSTDIR\mykids.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\MyKids\Панель MyKids.lnk"
  Delete "$SMPROGRAMS\MyKids\Проверка MyKids.lnk"
  Delete "$SMPROGRAMS\MyKids\Удалить MyKids.lnk"
  Delete "$SMPROGRAMS\MyKids\Задания и магазин.url"
  RMDir "$SMPROGRAMS\MyKids"
  Delete "$DESKTOP\Задания MyKids.url"

  DeleteRegKey HKLM "${UNINST_KEY}"
SectionEnd

; Журнал учёта по умолчанию остаётся: в нём история заработанного и
; потраченного. Стереть её молча — потерять данные, о которых никто не
; спрашивал.
Section /o "un.Журнал учёта и привязка" SecUnData
  DetailPrint "Удаление данных: $APPDATA\MyKids"
  RMDir /r "$APPDATA\MyKids"
SectionEnd

!insertmacro MUI_UNFUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecUnMain} "Служба, программы, ярлыки и запись в «Программах и компонентах»."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecUnData} "C:\ProgramData\MyKids: привязка к серверу и неотправленный учёт. Оставьте, если собираетесь поставить MyKids снова."
!insertmacro MUI_UNFUNCTION_DESCRIPTION_END

Function un.onInit
  SetRegView 64
  SetShellVarContext all
  ; Без окон данные удаляются только по прямой просьбе: /PURGE.
  ${un.GetParameters} $R0
  ClearErrors
  ${un.GetOptions} $R0 "/PURGE" $R1
  ${IfNot} ${Errors}
    SectionSetFlags ${SecUnData} ${SF_SELECTED}
  ${EndIf}
FunctionEnd
