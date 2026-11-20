# Сервер MyKids для Windows

`MyKids-Server-Setup-X.Y.Z.exe` — домашний сервер на обычном компьютере, без
Docker и без терминала.

```
C:\Program Files\MyKids Server\
  mykids-server.exe   служба и окно управления (agents/windows/cmd/mykids-server)
  node\node.exe       Node.js
  pgsql\              PostgreSQL: bin, lib, share — без pgAdmin и документации
  app\                API одним файлом (esbuild), клиент Prisma под Windows,
                      страницы, пакеты заданий, миграции
C:\ProgramData\MyKids Server\      доступ: SYSTEM, администраторы, служба
  config.json         порт, порт базы, случайный пароль базы
  pgdata\  logs\  backups\
```

Служба `MyKidsServer` работает под `NT AUTHORITY\LocalService`: PostgreSQL
отказывается работать с правами администратора. Она заводит базу при первом
запуске, применяет миграции (теми же файлами и с тем же учётом в
`_prisma_migrations`, что Prisma в образе Docker, — через psql из комплекта),
запускает API, перезапускает его при падении и раз в сутки делает резервную
копию. Восстановление из копии тоже делает она: окно управления кладёт
файл в `restore.dump` и перезапускает службу.

## Сборка

```bash
VERSION=0.9.1 deploy/server-windows/build.sh    # нужны Go, Node, unzip, makensis
```

Node.js и PostgreSQL скачиваются с официальных сайтов, версии и суммы
закреплены в `build.sh`.

## Проверка

```bash
deploy/server-windows/live-test.sh   # от обычного пользователя, не root
```

Собирает тот же комплект под эту машину и гоняет службу на настоящих
PostgreSQL и Node: пустой каталог → база → миграции → регистрация семьи →
остановка → повторный запуск с теми же данными. Идёт в CI на каждый коммит.
