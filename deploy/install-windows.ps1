<#
.SYNOPSIS
    Ставит агент MyKids на компьютер ребёнка.

.DESCRIPTION
    Запускать от имени администратора, в учётной записи родителя:

        powershell -ExecutionPolicy Bypass -File install-windows.ps1 `
            -Server http://домашний-сервер:3000 `
            -Token  <токен устройства из админки> `
            -ChildUser Марк

    Токен выдаётся на странице ребёнка кнопкой «Выдать токен» и показывается
    один раз.

    Скрипт ничего не удаляет и не создаёт учётных записей. Всё, что он делает,
    перечислено в выводе: скопировать .exe, завести каталог данных, привязать
    устройство, зарегистрировать службу.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [Parameter(Mandatory = $true)][string]$Token,
    # Учётная запись ребёнка. Нужна только для проверки прав: агент в обычной
    # учётной записи имеет смысл, в учётной записи администратора — нет.
    [string]$ChildUser,
    # Адрес заданий и магазина. По умолчанию — тот же сервер: страницы отдаёт
    # он сам. Если интерфейс вынесен отдельно, укажите адрес явно.
    [string]$ChildUrl,
    [string]$Agent   = (Join-Path $PSScriptRoot 'mykids-agent.exe'),
    [string]$Install = 'C:\Program Files\MyKids',
    [string]$Data    = (Join-Path $env:ProgramData 'MyKids')
)

$ErrorActionPreference = 'Stop'

function Require-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($id)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Нужны права администратора: служба регистрируется на всю машину.'
    }
}

# Агент в учётной записи администратора не защищает ни от чего: ребёнок просто
# остановит службу. Это не придирка к мелочи, а единственное условие, без
# которого вся остальная установка бессмысленна.
function Check-ChildAccount([string]$name) {
    if (-not $name) {
        Write-Warning 'Учётная запись ребёнка не названа (-ChildUser): проверить её права не могу.'
        Write-Warning 'Убедитесь сами, что она обычная, а не администратор.'
        return
    }
    $user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
    if (-not $user) {
        Write-Warning "Учётной записи «$name» на этой машине нет. Заведите её как обычную (не администратора)."
        return
    }
    $admins = Get-LocalGroupMember -Group (Get-LocalGroup -SID 'S-1-5-32-544').Name -ErrorAction SilentlyContinue
    if ($admins | Where-Object { $_.SID -eq $user.SID }) {
        throw @"
Учётная запись «$name» состоит в администраторах.
Ребёнок с правами администратора остановит службу из оснастки, и агент не
помешает ему ничем. Выведите её из группы «Администраторы» и повторите.
"@
    }
    Write-Host "Учётная запись «$name»: обычная, без прав администратора — так и надо."
}

# Каталог данных общий для службы и родителя, но ребёнку в нём делать нечего:
# в нём лежат состояние учёта и очередь расхода. Наследование прав снимаем,
# иначе ребёнок сможет создавать там файлы.
function New-DataDir([string]$path) {
    New-Item -ItemType Directory -Force -Path $path | Out-Null

    $acl = Get-Acl $path
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($rule in @($acl.Access)) { $acl.RemoveAccessRule($rule) | Out-Null }
    foreach ($sid in @('S-1-5-18', 'S-1-5-32-544')) {   # SYSTEM, Администраторы
        $who = [Security.Principal.SecurityIdentifier]::new($sid)
        $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new(
            $who, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow'))
    }
    Set-Acl -Path $path -AclObject $acl
    Write-Host "Каталог данных: $path (доступ только службе и администраторам)"
}

Require-Admin
if (-not (Test-Path $Agent)) {
    throw "Не найден mykids-agent.exe: $Agent. Скачайте его со страницы релиза и положите рядом."
}
Check-ChildAccount $ChildUser
if (-not $ChildUrl) { $ChildUrl = "$($Server.TrimEnd('/'))/child" }

New-Item -ItemType Directory -Force -Path $Install | Out-Null
$exe = Join-Path $Install 'mykids-agent.exe'
Copy-Item -Path $Agent -Destination $exe -Force
Write-Host "Агент: $exe"

New-DataDir $Data

# Привязка идёт от администратора, а работает под LocalSystem — поэтому
# каталог данных машинный, а не в профиле. Иначе служба не увидела бы токен
# и молча работала бы автономно.
& $exe -data $Data enroll -server $Server -token $Token -child-url $ChildUrl
if ($LASTEXITCODE -ne 0) { throw 'Привязка не удалась: токен неверен или сервер недоступен.' }

& $exe -data $Data service install
if ($LASTEXITCODE -ne 0) { throw 'Не удалось зарегистрировать службу.' }
& $exe -data $Data service start
if ($LASTEXITCODE -ne 0) { throw 'Служба зарегистрирована, но не запустилась. Смотрите журнал событий.' }

Write-Host ''
Write-Host 'Готово. Что проверить прямо сейчас:'
Write-Host "  1. $exe -data $Data status  — видит ли активное окно и есть ли связь"
Write-Host '  2. Войдите в учётную запись ребёнка: помощника служба поднимет сама'
Write-Host "  3. Откройте $ChildUrl — там задания и магазин"
Write-Host ''
Write-Host 'Удаление:'
Write-Host "  $exe -data $Data service stop; $exe -data $Data service uninstall"
