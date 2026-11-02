# Быстрый старт

Скачайте нужные файлы из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases).

> # !!! ВАЖНО: один Creator = один Joiner !!!
>
> Одна сессия Creator (одна вкладка в десктопе или один headless-процесс) обслуживает **ровно одного** Joiner-а. Нельзя подключить нескольких Joiner-ов к одной ссылке - туннель сломается. Для каждого устройства поднимайте **отдельный** Creator со своей ссылкой.

## Содержание

- [Что нужно](#что-нужно)
- [Creator (десктоп)](#creator-десктоп)
- [Creator (headless, сервер)](#creator-headless-сервер)
- [Бот VK](#бот-vk)
- [Бот VK (headless, сервер)](#бот-vk-headless-сервер)
- [Joiner (Android)](#joiner-android)
- [Joiner (iOS)](#joiner-ios)
- [Joiner (Десктоп: Windows / macOS / Linux)](#joiner-десктоп-windows--macos--linux)
- [Joiner (Linux, headless)](#joiner-linux-headless)
- [Если не работает](#если-не-работает)

## Что нужно

- **Creator** (сторона со свободным интернетом) - десктоп или headless на сервере
- **Joiner** (сторона с цензурой) - Android, iOS, десктоп (Windows / macOS / Linux) или консольный бинарник для Linux / FreeBSD

Два режима туннеля: **DC** (DataChannel) и **Video** (VP8). Headless creator автоматически подстраивается под режим, выбранный joiner-ом, при каждом новом подключении. В legacy браузерном пути такого нет - нельзя создать DC в VK и подключиться по видео.

В режиме **Video** на joiner-е можно включить **Reliable (KCP)**: потерянные кадры переотправляются, и TCP-соединения не рвутся на сети с потерями. Creator определяет это сам по первому кадру joiner-а, на его стороне ничего настраивать не нужно. Работает с VK, Telemost, WB Stream, Bitrix и Blink.

> **Рекомендуется headless с обеих сторон** - там есть обфускация траффика, настраиваемый VP8 pacing и WB Stream. Старый браузерный путь (Android `WebView` против Electron-creator с JS-хуками) ещё работает, но медленнее, без обфускации и постепенно выводится из эксплуатации.

> **Рекомендация: лучше запускать Creator на домашнем ПК.** Некоторые операторы и платформы могут заметить, что звонок идёт с IP дата-центра (VPS / хостинг), и заблокировать аккаунт. На VPS всё работает - но если дорожите аккаунтом, запускайте Creator с домашнего интернета либо используйте отдельный / одноразовый аккаунт. Касается обоих серверных вариантов - headless и десктопного Creator под XPRA.

## Creator (десктоп)

![Интерфейс](res/desktop_interface.png)

1. Скачайте и запустите приложение
2. Нажмите **+** для создания вкладки
3. Выберите платформу: **VK** / **Telemost** / **WBStream** / **DION** / **Bitrix** / **Blink**
4. Нажмите **Create new**, либо **Join existing**, чтобы подключиться к уже созданному звонку по ссылке
5. При первом запуске платформы приложение само откроет окно логина - авторизуйтесь
6. Скопируйте ссылку на звонок и отправьте на Joiner

Это headless режим - звонок создаётся без браузера, режим туннеля выбирает joiner. Ссылку можно отметить звёздочкой, она появится в **Saved calls** на стартовом экране.

Кнопки **VK (legacy)** и **Telemost (legacy)** - старый браузерный путь: режим **DC** (только VK) или **Video** выбирается в панели вкладки, звонок нужно создавать из приложения.

Если нужно запустить headless на сервере без GUI - нажмите **Export Cookies**, получите `cookies.zip` со всеми шестью файлами и используйте их с headless бинарниками.

> Запуск десктопного Creator на VPS без графического окружения через XPRA - см. [docs/vps/SETUP.md](vps/SETUP.md).

## Creator (headless, сервер)

Для запуска Creator на сервере без GUI.

### Какой архив скачать

Все консольные бинарники - шесть creator-ов, пять joiner-ов и `headless-vk-bot` - лежат в [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases) одним архивом на систему:

| Архив | Система |
|---|---|
| `whitelist-bypass-cli-linux-x64.zip` | Linux x64 - обычный VPS или ПК |
| `whitelist-bypass-cli-linux-ia32.zip` | Linux x86 (32 бита) |
| `whitelist-bypass-cli-linux-arm.zip` | Linux ARM: папка `arm64/` для 64-битной системы, `arm/` для 32-битной |
| `whitelist-bypass-cli-linux-mips.zip` | Роутеры на MIPS: папки `mips/`, `mipsle/`, `mips64/`, `mips64le/` |
| `whitelist-bypass-cli-freebsd-x64.zip` | FreeBSD x64 |
| `whitelist-bypass-cli-freebsd-arm64.zip` | FreeBSD ARM64 |

Внутри бинарники называются без суффикса архитектуры: `headless-vk-creator`, `headless-telemost-joiner` и т.д.

### Подготовка кук

Куки нужны для авторизации на платформе, их требуют все шесть. Экспортируйте их из десктопного Creator:

1. Откройте Creator на десктопе
2. Авторизуйтесь в нужных платформах - проще всего один раз запустить headless-вкладку каждой, приложение само попросит логин
3. Нажмите **Export Cookies** - получится `cookies.zip` с файлами `cookies-vk.json`, `cookies-yandex.json`, `cookies-wbstream.json`, `cookies-dion.json`, `cookies-bitrix.json`, `cookies-blink.json`
4. Распакуйте и скопируйте нужные файлы на сервер

`cookies-dion.json`, `cookies-bitrix.json` и `cookies-blink.json` creator перезаписывает на ходу, когда обновляет токен или перелогинивается - эти три файла должны быть доступны процессу на запись. VK, Telemost и WB Stream только читаются.

> **Bitrix:** портал, зарегистрированный на `gmail.com`, падает при создании звонка с `403 invalid signature`. Заводите портал на другой почте: проверены `mail.ru` и `onetimemail.com.au`.

### DION: логин по паролю

У DION refresh-токен одноразовый: при каждом обновлении сервер выдаёт новый и убивает старый, а процесс сразу перезаписывает `cookies-dion.json`. Из этого следуют два правила:

- файл должен быть доступен процессу на запись, и его нельзя подменять старой копией;
- одна копия файла - один процесс. Если запустить тот же файл локально и на сервере, две копии будут убивать цепочку друг другу.

Чтобы не переносить куки заново, положите в файл email и пароль - тогда процесс сам залогинится, когда refresh-токен всё же умрёт:

```json
{
  "email": "you@example.com",
  "password": "secret",
  "cookies": []
}
```

Такого файла достаточно для старта, куки допишутся сами после первого логина. В десктопном Creator то же самое делается кнопкой **Edit credentials** на карточке **DION account** - она появляется на DION-вкладке до запуска звонка, и после этого `Export Cookies` кладёт в зип уже готовый файл с логином.

Старый формат - голый массив `[{"name": "...", "value": "..."}]` - по-прежнему читается.

### Bitrix: логин по паролю

В `cookies-bitrix.json` кроме кук лежат адрес портала и учётка:

```json
{
  "portal": "https://b24-xxxxxx.bitrix24.ru",
  "email": "you@example.com",
  "password": "secret",
  "cookies": []
}
```

`portal` обязателен. Когда сессия умирает, creator логинится сам через `auth2.bitrix24.net` и перезаписывает файл новыми куками. В десктопном Creator то же самое делается кнопкой **Edit credentials** на карточке **Bitrix account** - она появляется на Bitrix-вкладке до запуска звонка.

### Blink: вход через приложение

У Blink нет гостевого входа и входа по паролю. Войти можно прямо на сервере, без десктопного Creator:

```sh
./headless-blink-creator --login --cookies cookies-blink.json
```

Процесс выведет ссылку. Откройте её на телефоне, где установлено приложение Blink, и подтвердите вход - токены сохранятся в файл, процесс завершится.

Refresh-токен у Blink одноразовый, как у DION: при каждом обновлении процесс перезаписывает файл. Поэтому файл должен быть доступен на запись, и одна копия файла - один процесс. Если обновить токен не получилось, запустите `--login` ещё раз.

Creator и joiner должны входить **отдельно** - один вход нельзя использовать на двух устройствах сразу, Blink выкинет одно из них. Аккаунт при этом может быть один.

### Запуск

```sh
# VK
./headless-vk-creator --cookies cookies-vk.json

# Telemost
./headless-telemost-creator --cookies cookies-yandex.json

# WB Stream
./headless-wbstream-creator --cookies cookies-wbstream.json

# DION
./headless-dion-creator --cookies cookies-dion.json

# Bitrix
./headless-bitrix-creator --cookies cookies-bitrix.json

# Blink
./headless-blink-creator --cookies cookies-blink.json
```

После запуска Creator создаст звонок и выведет ссылку в лог. Ссылку нужно передать на Joiner.

### Подключение к существующему звонку

Чтобы не пересоздавать звонок при перезапуске, передайте существующую ссылку:

```sh
./headless-vk-creator       --cookies cookies-vk.json     --vk-link https://vk.ru/call/join/<token>
./headless-telemost-creator --cookies cookies-yandex.json --tm-link https://telemost.yandex.ru/j/<id>
./headless-wbstream-creator --cookies cookies-wbstream.json --room wbstream://<uuid>
./headless-dion-creator     --cookies cookies-dion.json    --room dion://<id>
./headless-bitrix-creator   --cookies cookies-bitrix.json --room https://<portal>.bitrix24.ru/video/<alias>
./headless-blink-creator    --cookies cookies-blink.json  --room blink://<chat id>
```

### Флаги

| Флаг | VK | TM | WB | DION | BX | BL | Описание |
|---|---|---|---|---|---|---|---|
| `--cookies <path>` | да | да | да | да | да | да | Путь к файлу с куками (JSON) |
| `--cookie-string <str>` | да | да | - | - | - | - | Куки строкой (`name=val; name=val`) |
| `--login` | - | - | - | - | - | да | Войти через приложение Blink, сохранить токены в `--cookies` и выйти |
| `--peer-id <id>` | да | - | - | - | - | - | VK peer_id для нового звонка |
| `--vk-link <link>` | да | - | - | - | - | - | Подключиться к существующему VK звонку |
| `--tm-link <uri>` | - | да | - | - | - | - | Подключиться к существующей Telemost конференции |
| `--room <id>` | - | - | да | да | да | да | Подключиться к существующей комнате: `wbstream://<id>` для WB, `dion://<id>` для DION, ссылка `https://<portal>/video/<alias>` или один алиас для Bitrix, `blink://<chat id>`, id чата или ссылка-приглашение в группу для Blink |
| `--name <name>` | - | - | да | да | - | - | Отображаемое имя в комнате |
| `--resources <mode>` | да | да | да | да | да | да | `default` / `moderate` / `unlimited` / `custom`; у DION без `custom` |
| `--read-buf <bytes>` | да | да | да | - | да | да | Размер read-буфера, только с `--resources custom` |
| `--max-dc-buf <bytes>` | да | - | - | - | - | - | Порог `BufferedAmountLowThreshold` DC, только с `--resources custom` |
| `--mem-limit <bytes>` | да | да | да | - | - | да | Soft memory limit Go рантайма, только с `--resources custom` |
| `--write-file <path>` | да | да | да | да | да | да | Файл, куда записывается активная ссылка на звонок |
| `--upstream-socks <host:port>` | да | да | да | да | да | да | Гнать трафик joiner-а через локальный SOCKS5 прокси, например VPN-клиент |
| `--upstream-user <user>` | да | да | да | да | да | да | Логин для upstream SOCKS5 |
| `--upstream-pass <pass>` | да | да | да | да | да | да | Пароль для upstream SOCKS5 |
| `--allow-private-dst` | да | да | да | да | да | да | Разрешить joiner-у ходить в локальную сеть creator-а, по умолчанию такие адреса блокируются. Loopback этим флагом не открывается, для него есть `--allow-loopback` |
| `--allow-loopback` | да | да | да | да | да | да | Разрешить joiner-у ходить на `127.0.0.1` самого creator-а, например в локальный инбаунд xray. По умолчанию выключено, от `--allow-private-dst` не зависит. Открывает доступ ко всем сервисам, слушающим loopback на этой машине |
| `--debug` | да | да | да | да | да | да | Подробные логи |
| `--version` | да | да | да | да | да | да | Вывести версию и выйти |

### Выходной трафик через свой VPS

По умолчанию creator открывает соединения joiner-а напрямую со своего IP. Флаг `--upstream-socks` заставляет creator отправлять **только трафик joiner-а** в локальный SOCKS5 прокси - обычно его поднимает VPN-клиент (xray / sing-box / v2ray и т.п.) рядом с creator. Получается цепочка:

```
joiner (цензура) -> creator (домашний ПК) -> VPN-клиент (тот же ПК) -> VPS -> интернет
```

SOCKS5-инбаунд VPN-клиента должен поддерживать UDP ASSOCIATE (xray / sing-box умеют).

```sh
./headless-telemost-creator \
  --cookies cookies-yandex.json \
  --tm-link https://telemost.yandex.ru/j/<id> \
  --upstream-socks 127.0.0.1:1080
```

### Режимы ресурсов

| Режим | `read-buf` | `max-dc-buf` (VK) | `mem-limit` | Когда использовать |
|---|---|---|---|---|
| `moderate` | 16 KB | 1 MB | 64 MB | VPS с малой RAM |
| `default`  | 32 KB | 4 MB | 128 MB | Обычное использование |
| `unlimited`| 64 KB | 8 MB | 256 MB | Максимум пропускной способности (может троттлить из-за congestion control) |
| `custom`   | из `--read-buf` | из `--max-dc-buf` | из `--mem-limit` | Тонкая настройка |

В режиме `custom` любой не указанный флаг использует значения из `unlimited`. Пример с явной настройкой всех буферов:

```sh
./headless-vk-creator \
  --cookies cookies-vk.json \
  --vk-link https://vk.ru/call/join/<token> \
  --write-file /var/run/whitelist-bypass/call.txt \
  --resources custom \
  --read-buf 65536 \
  --max-dc-buf 8388608 \
  --mem-limit 268435456
```

## Бот VK

Бот позволяет создавать звонки через сообщения ВКонтакте без прямого доступа к Creator.

### Настройка

1. Создайте сообщество ВКонтакте (можно приватное)

![Создание сообщества](res/create_community.png)

2. Перейдите в "Управление" сообщества

![Управление](res/management.png)

3. Раздел "Дополнительно" -> "Работа с API" -> "Создать ключ", проставьте все галочки

![API](res/api_section.png)
![Создание ключа](res/create_key.png)
![Создание ключа](res/create_key1.png)

4. Подтвердите SMS и скопируйте ключ (повторное копирование потребует SMS)

![Копирование ключа](res/copy_key.png)

5. Включите Long Poll API, в "Типы событий" -> "Сообщения" проставьте все галочки

![Long Poll API](res/long_poll_api1.png)

6. "Сообщения" -> "Настройки для бота" -> включите "Возможности ботов"

![Настройки бота](res/bot_settings.png)

7. Скопируйте ID сообщества (только цифры, без "club")

![ID сообщества](res/community_id.png)

8. Узнайте свой VK ID (профиль -> "Управление аккаунтом VK ID" -> "Мои данные"). Чтобы разрешить нескольким пользователям, перечислите ID через запятую (например `12345,67890`).

9. Заполните настройки бота в Creator: Token, Group ID, User IDs (через запятую). Сохраните и включите бота. Бот игнорирует команды от пользователей не из списка.

![Настройки бота в Creator](res/bot_config.png)

### Команды

- `/vk headless` - звонок VK, headless режим
- `/tm headless` - звонок Telemost, headless режим
- `/wb headless` - комната WB Stream
- `/dion` - звонок DION
- `/bitrix` - звонок Bitrix
- `/blink` - звонок Blink
- `/list` - список активных вкладок
- `/close <id>` - закрыть вкладку по ID

#### Legacy (браузерный путь, выводится из эксплуатации)

- `/vk dc` - звонок VK, режим DC
- `/vk video` - звонок VK, режим Video
- `/tm video` - звонок Telemost, режим Video

![Команды бота](res/bot_commands.jpeg)

## Бот VK (headless, сервер)

Standalone Go-бинарник `headless-vk-bot` - то же самое, что бот в десктопном Creator, но без Electron. Слушает Long Poll сообщества, по команде запускает соответствующий headless-creator и присылает в чат join-link. Подходит для VPS: один процесс держит Long Poll и запускает creators по запросу.

### Подготовка

1. Создайте сообщество ВКонтакте и получите community access token, group ID и свой VK ID - порядок описан в разделе [Бот VK](#бот-vk) (шаги 1-8).
2. Скачайте архив под свою систему (см. [Какой архив скачать](#какой-архив-скачать)) и распакуйте его в одну папку - например `/usr/local/bin/`:

   ```sh
   sudo unzip whitelist-bypass-cli-linux-x64.zip -d /usr/local/bin/
   sudo chmod +x /usr/local/bin/headless-*
   ```

   В архиве лежат `headless-vk-bot` и все creator-бинарники под обычными именами (`headless-vk-creator`, `headless-telemost-creator` и т.д.) - именно их бот ищет в `--bins-dir`.
3. Подготовьте куки - все шесть платформ требуют залогиненную сессию. В десктопном Creator нажмите **Export Cookies** - получите `cookies.zip` со всеми шестью файлами (`cookies-vk.json`, `cookies-yandex.json`, `cookies-wbstream.json`, `cookies-dion.json`, `cookies-bitrix.json`, `cookies-blink.json`). Для Blink можно вместо экспорта войти прямо на сервере, см. [Blink: вход через приложение](#blink-вход-через-приложение).
4. Распакуйте архив и скопируйте куки на сервер.

### Запуск

```sh
./headless-vk-bot \
  --token <community_access_token> \
  --group-id <community_id> \
  --user-id <vk_id_1>,<vk_id_2> \
  --bins-dir /usr/local/bin \
  --vk-cookies /etc/whitelist-bypass/cookies-vk.json \
  --tm-cookies /etc/whitelist-bypass/cookies-yandex.json \
  --wb-cookies /etc/whitelist-bypass/cookies-wbstream.json \
  --dion-cookies /etc/whitelist-bypass/cookies-dion.json \
  --bitrix-cookies /etc/whitelist-bypass/cookies-bitrix.json \
  --blink-cookies /etc/whitelist-bypass/cookies-blink.json
```

### Флаги

| Флаг | Описание |
|---|---|
| `--token <str>` | Community access token (обязательно) |
| `--group-id <id>` | ID сообщества, только цифры (обязательно) |
| `--user-id <ids>` | Список VK ID через запятую (`12345,67890`), которым разрешено отправлять команды. Пусто = разрешено всем (НЕ рекомендуется) |
| `--bins-dir <dir>` | Папка, где лежат `headless-vk-creator` / `headless-telemost-creator` / `headless-wbstream-creator` / `headless-dion-creator` / `headless-bitrix-creator` / `headless-blink-creator` (обязательно) |
| `--vk-cookies <path>` | Путь к VK куки (нужен для `/vk` и для join по VK call-ссылке) |
| `--tm-cookies <path>` | Путь к Yandex куки (нужен для `/tm` и для join по Telemost-ссылке) |
| `--wb-cookies <path>` | Путь к WB Stream куки (нужен для `/wb` и для join по `wbstream://` / `stream.wb.ru` ссылке) |
| `--dion-cookies <path>` | Путь к DION куки (нужен для `/dion` и для join по `dion://` / `dion.vc` ссылке) |
| `--bitrix-cookies <path>` | Путь к Bitrix куки (нужен для `/bitrix` и для join по ссылке `bitrix24` + `/video/`) |
| `--blink-cookies <path>` | Путь к Blink куки (нужен для `/blink` и для join по `blink://` / `blinkmap.com` + `/join/` ссылке) |
| `--sessions-dir <dir>` | Папка для логов запущенных creators. Опционально - без флага логи не пишутся, stdout/stderr creators отбрасываются |
| `--resources <mode>` | Режим ресурсов, передаётся каждому запускаемому creator: `default` / `moderate` / `unlimited`. По умолчанию `default`. `custom` не поддерживается, так как у каждого бинарника свой набор флагов настройки |
| `--allow-private-dst` | Передаётся каждому запускаемому creator: разрешить joiner-у ходить в локальную сеть creator-а. По умолчанию выключено |
| `--allow-loopback` | Передаётся каждому запускаемому creator: разрешить joiner-у ходить на его `127.0.0.1`. По умолчанию выключено |
| `--version` | Вывести версию и выйти |

При получении команды бот запускает соответствующий creator с `--write-file <tmp>`, ждёт появления линка в файле (до 60 секунд) и присылает его в чат. `--user-id` принимает список через запятую; если указан, команды от пользователей не из списка игнорируются.

`--token` и `--upstream-pass` передавать через переменные окружения `VK_TOKEN` и `UPSTREAM_PASS`. Через окружение секреты не попадают в `ps` и `/proc/PID/cmdline`, которые читает любой пользователь на хосте, - `/proc/PID/environ` доступен только владельцу процесса.

### Запуск как systemd-сервис

`/etc/systemd/system/wlb-vk-bot.service`:

```ini
[Unit]
Description=Headless VK bot (whitelist-bypass)
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/headless-vk-bot \
  --token <community_access_token> \
  --group-id <community_id> \
  --user-id <vk_id_1>,<vk_id_2> \
  --bins-dir /usr/local/bin \
  --vk-cookies /etc/whitelist-bypass/cookies-vk.json \
  --tm-cookies /etc/whitelist-bypass/cookies-yandex.json \
  --wb-cookies /etc/whitelist-bypass/cookies-wbstream.json \
  --dion-cookies /etc/whitelist-bypass/cookies-dion.json \
  --bitrix-cookies /etc/whitelist-bypass/cookies-bitrix.json \
  --blink-cookies /etc/whitelist-bypass/cookies-blink.json
Restart=always
RestartSec=5
User=wlb

[Install]
WantedBy=multi-user.target
```

Активировать:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now wlb-vk-bot
sudo journalctl -u wlb-vk-bot -f
```

При остановке сервиса бот посылает `SIGTERM` всем запущенным creator-процессам, чтобы не оставлять незакрытых сессий.

### Запуск через Docker

Альтернатива systemd. Готовый образ публикуется в GHCR (`ghcr.io/kulikov0/whitelist-bypass-bot`) - под капотом тот же `headless-vk-bot` плюс шесть creator-бинарников. Поддерживаемые архитектуры: `linux/amd64`, `linux/arm64`, `linux/386`.

```sh
mkdir wlb-bot && cd wlb-bot
curl -O https://raw.githubusercontent.com/kulikov0/whitelist-bypass/main/headless/docker/docker-compose.yml
curl -L https://raw.githubusercontent.com/kulikov0/whitelist-bypass/main/headless/docker/.env.example -o .env
# отредактируйте .env: VK_TOKEN, VK_GROUP_ID, VK_USER_IDS
# положите рядом cookies-vk.json, cookies-yandex.json, cookies-wbstream.json, cookies-dion.json, cookies-bitrix.json, cookies-blink.json
# (для платформ, которые не используете - создайте файл с содержимым `[]`)
sudo chown 999:999 cookies-dion.json cookies-bitrix.json cookies-blink.json
docker compose up -d
docker compose logs -f
```

Обновление до новой версии:

```sh
docker compose pull && docker compose up -d
```

| Переменная | Обязательна | По умолчанию | Соответствует флагу |
|---|---|---|---|
| `VK_TOKEN` | да | - | `--token` |
| `VK_GROUP_ID` | да | - | `--group-id` |
| `VK_USER_IDS` | нет | пусто (всем) | `--user-id` |
| `RESOURCES` | нет | `default` | `--resources` |
| `BINS_DIR` | нет | `/opt/wlb/bin` | `--bins-dir` |
| `SESSIONS_DIR` | нет | `/data/sessions` | `--sessions-dir` |
| `VK_COOKIES` | нет | `/data/cookies-vk.json` если есть | `--vk-cookies` |
| `TM_COOKIES` | нет | `/data/cookies-yandex.json` если есть | `--tm-cookies` |
| `WB_COOKIES` | нет | `/data/cookies-wbstream.json` если есть | `--wb-cookies` |
| `DION_COOKIES` | нет | `/data/cookies-dion.json` если есть | `--dion-cookies` |
| `BITRIX_COOKIES` | нет | `/data/cookies-bitrix.json` если есть | `--bitrix-cookies` |
| `BLINK_COOKIES` | нет | `/data/cookies-blink.json` если есть | `--blink-cookies` |
| `UPSTREAM_SOCKS` | нет | - | `--upstream-socks` |
| `UPSTREAM_USER` | нет | - | `--upstream-user` |
| `UPSTREAM_PASS` | нет | - | `--upstream-pass` |
| `ALLOW_PRIVATE_DST` | нет | `0` | `--allow-private-dst`, включается значением `1` |
| `ALLOW_LOOPBACK_DST` | нет | `0` | `--allow-loopback`, включается значением `1`. В контейнере это loopback самого контейнера, если не задан `network_mode: host` |

> `cookies-dion.json`, `cookies-bitrix.json` и `cookies-blink.json` монтируются без `:ro` - creator перезаписывает их при обновлении токена, поэтому на хосте они должны принадлежать uid 999 (иначе `save cookies failed: permission denied`). VK, Telemost и WB Stream монтируются с `:ro`.

> Если WebRTC-туннель не доходит через сетевой бридж Docker (UDP может отбрасываться), добавьте в `docker-compose.yml` строку `network_mode: host` под сервисом `bot`.

> `UPSTREAM_SOCKS` гонит трафик joiner-а через локальный SOCKS5 (VPN-клиент), см. [Выходной трафик через свой VPS](#выходной-трафик-через-свой-vps). Внутри контейнера `127.0.0.1` - это сам контейнер, а не хост: чтобы дотянуться до VPN-клиента на хосте, используйте `host.docker.internal:<порт>` и раскомментируйте `extra_hosts` в `docker-compose.yml`, либо поднимите VPN-клиент отдельным сервисом и укажите его имя.

### Команды

- `/vk` - запустить `headless-vk-creator`
- `/tm` - запустить `headless-telemost-creator`
- `/wb` - запустить `headless-wbstream-creator`
- `/dion` - запустить `headless-dion-creator`
- `/bitrix` - запустить `headless-bitrix-creator`
- `/blink` - запустить `headless-blink-creator`
- `/list` - список активных сессий
- `/close <id>` - закрыть сессию по короткому ID
- `/start` - показать главное меню

Все команды также доступны через inline-клавиатуру в чате.

## Joiner (Android)

![Интерфейс](res/android_interface.png)

1. Скачайте и установите `whitelist-bypass.apk`
2. На вкладке **Main** добавьте звонок: вставьте ссылку (кнопка **Paste from clipboard** или скан QR из Creator) и нажмите **Save call**. Платформа определится по ссылке сама
3. На вкладке **Settings** выберите **Tunnel mode** (**DC** или **Video**). Headless-creator подстроится под выбранный режим автоматически
4. Выберите звонок в списке и нажмите **Connect**
5. При первом подключении разрешите VPN-подключение в системном диалоге
6. Дождитесь статуса **Connected** - весь трафик устройства теперь идет через звонок

### Blink

У Blink нет гостевого входа, поэтому при первом подключении по Blink-ссылке приложение покажет окно **Log in to Blink** с QR-кодом и ссылкой:

- Blink стоит на этом же телефоне - нажмите **Open in Blink** и подтвердите вход в приложении Blink
- Blink на другом телефоне - отсканируйте QR его приложением Blink, либо нажмите **Copy link** и отправьте ссылку тому, у кого есть Blink

После подтверждения подключение продолжится само. Вход сохраняется; если Blink сбросит сессию, окно входа появится снова. Joiner входит отдельно от Creator, аккаунт при этом может быть тот же.

### Настройки

**Tunnel**
- **Tunnel mode** - режим туннеля (DC / Video)
- **VP8 settings** - FPS, Batch, **Dual track** и **Reliable (KCP)** для режима Video (см. ниже)
- **Autofill** - имя, с которым joiner входит в звонок; в WebView-режиме ещё и сам нажимает кнопку входа

**Network**
- **Split tunneling** - выбор приложений, которые пойдут через туннель
- **Proxy** - хост и порт SOCKS5, авторизация. По умолчанию `127.0.0.1` (только локально); поставьте `0.0.0.0` чтобы прокси был доступен другим устройствам в локальной сети. **Proxy only** - без VPN, только прокси
- **DNS** - **System** или **Custom** DNS

**Behavior**
- **Headless mode** - подключение без WebView (рекомендуется, включен по умолчанию)
- **Reconnect on app start** - автоподключение к последнему звонку при запуске
- **Debug logging** - подробные логи для отладки, сами логи на вкладке **Logs**
- **Power optimization** - попросить систему не усыплять приложение

**Appearance** - тема. **Danger zone** - **Forget all calls** (удалить сохранённые ссылки) и **Reset all settings**.

### VP8 settings

Управляет тем, как часто joiner отправляет VP8-кадры через SFU. Настраивается только на joiner; creator подстраивается под значения, переданные joiner-ом, в начале сессии.

- **FPS** - номинальная частота кадров VP8. Диапазон 1..240, по умолчанию 24.
- **Batch** - множитель плотности тиков. Реальная скорость отправки ≈ `fps × batch` кадров/сек. Диапазон 1..256, по умолчанию 30 (≈6.5 Mbps теоретического потолка).
- **Dual track** - публиковать второй VP8-трек как демонстрацию экрана и делить исходящий трафик между двумя треками.
- **Reliable (KCP)** - обернуть видеотуннель в KCP: потерянные кадры переотправляются. Без него - чистый VP8, быстрее, но потери рвут соединения.

Throughput ≈ `fps × batch × 1126 байт/кадр`. Примеры:

| fps | batch | потолок |
|----:|------:|--------:|
| 24  | 1     | ~27 KB/s |
| 24  | 8     | ~216 KB/s |
| 24  | 30    | ~810 KB/s (≈6.5 Mbps) |

Чем выше batch, тем больше нагрузка на CPU телефона и SFU. Если в логах появляются дропы пакетов или соединение нестабильно - уменьшите batch.

> **Важно:** обфускация и VP8 pacing работают только в режиме **headless-headless** - и creator, и joiner должны быть в headless-режиме.

## Joiner (iOS)

### VPN-приложение

`whitelist-bypass-vpn.ipa` поднимает системный VPN, как Android-приложение. Ему нужен Network Extension, а эту возможность даёт только платный аккаунт Apple Developer. С бесплатным аккаунтом ставьте [прокси-приложение](#прокси-приложение).

1. Скачайте `whitelist-bypass-vpn.ipa` (unsigned) из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases) и подпишите своим аккаунтом, либо соберите из исходников в Xcode (см. README)
2. На вкладке **Main** добавьте звонок: вставьте ссылку (**Paste from clipboard** или скан QR) и нажмите **Save call**
3. На вкладке **Settings** выберите **Tunnel mode** (**DC** или **Video**)
4. Выберите звонок и нажмите **Connect**. При первом подключении iOS попросит разрешить добавление конфигурации VPN
5. Дождитесь статуса **Connected** - весь трафик устройства идёт через звонок

Настройки те же, что на Android: **VP8 settings** с **Dual track** и **Reliable (KCP)**, **Autofill**, **Proxy**, **DNS**, **Reconnect on app start**, **Debug logging**.

### Прокси-приложение

`whitelist-bypass-proxy.ipa` поднимает только SOCKS5 прокси и ставится с бесплатным аккаунтом. Чтобы пустить через него весь трафик устройства, нужно VPN-приложение с поддержкой SOCKS5 (например Happ, Shadowrocket или Streisand).

1. Скачайте `whitelist-bypass-proxy.ipa` (unsigned) из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases) и установите его. Подробная инструкция по установке unsigned IPA на iPhone: [habr.com/ru/articles/1013990/](https://habr.com/ru/articles/1013990/). Либо соберите из исходников (см. README).
2. Установите любое VPN-приложение с поддержкой SOCKS5 (Happ, Shadowrocket, Streisand и т.п.).
3. Откройте whitelist-bypass, выберите режим туннеля (**DC** или **Video**), вставьте ссылку на звонок и нажмите **Go**. Headless-creator подстроится под выбранный режим автоматически.
4. Дождитесь статуса "Tunnel Active". Приложение покажет адрес SOCKS5 прокси (например `socks5://user:pass@127.0.0.1:1081`).
5. Скопируйте параметры SOCKS5 из whitelist-bypass.
6. Вставьте их в VPN-приложение и подключитесь - весь трафик устройства пойдет через туннель.

Для Blink-ссылки при первом подключении появится окно **Log in to Blink** - так же, как на Android, см. [Blink](#blink).

#### Подключение через Happ

Happ - бесплатное приложение из App Store, поддерживающее SOCKS5 в формате v2ray.

1. В whitelist-bypass дождитесь "Tunnel Active" и нажмите кнопку **Copy v2ray URL** - в буфер обмена скопируется ссылка вида `socks://...@127.0.0.1:1081#WLB-1081`.

   ![whitelist-bypass: Copy v2ray URL](res/ios_interface.jpeg)

2. Откройте Happ, нажмите **+** в правом верхнем углу и выберите **Import from clipboard**.

   ![Happ: Import from clipboard](res/ios_happ_s1.jpeg)

3. Сервер `WLB-1081` появится в списке - включите туннель.

   ![Happ: запуск туннеля](res/ios_happ_s2.jpeg)

> Приложение работает без NetworkExtension - это позволяет устанавливать его через бесплатный Apple Developer аккаунт (без подписки $99/год). Обратная сторона: порт SOCKS5 может меняться между запусками whitelist-bypass - если предыдущий порт занят, приложение выбирает свободный. Если в Happ туннель перестал работать после перезапуска - удалите сервер `WLB-...` и заново нажмите **Copy v2ray URL** + **Import from clipboard**.

Альтернативно прокси можно прописать в отдельных приложениях напрямую:
- **Telegram**: Настройки -> Данные и память -> Прокси -> Добавить прокси -> SOCKS5
- Или нажмите в whitelist-bypass кнопку **"Open in Telegram"** для автоматической настройки

#### Настройки прокси-приложения

- **Mode** - режим туннеля (DataChannel / Video (VP8))
- **Auth Mode** - авторизация прокси (Auto - случайные креденшалы, Manual - свои)
- **Display Name** - имя при входе в звонок
- **VP8 settings** - FPS, Batch, **Dual track** и **Reliable (KCP)**, см. [VP8 settings](#vp8-settings)
- **Show Logs** и **Debug logging** - логи для отладки

## Joiner (Десктоп: Windows / macOS / Linux)

GUI joiner для десктопа. Поддерживает все шесть платформ: **VK**, **Telemost**, **WB Stream**, **DION**, **Bitrix** и **Blink**. Автоматически определяет платформу по ссылке.

![Интерфейс](res/joiner_desktop_interface.jpeg)

В отличие от Linux headless joiner ниже, десктоп-joiner может поднять системный VPN-туннель: TUN-адаптер плюс split-default route, через который выходит весь трафик хоста (как Android VPN-режим). На Windows используется wintun, на Linux - `/dev/net/tun` + `iproute2`, на macOS - utun + `route(8)`/`ifconfig`. Если TUN не нужен - есть галочка **SOCKS5 only**, поднимается только локальный прокси.

### Скачать

Из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases):

- `WhitelistBypass.Joiner-<version>-x64.exe` / `-ia32.exe` - Windows (portable, без установщика)
- `WhitelistBypass.Joiner-<version>-x86_64.AppImage` - Linux
- `WhitelistBypass.Joiner-<version>-arm64.dmg` / `-x64.dmg` - macOS на Apple Silicon / Intel

### Запуск

1. Вставьте ссылку на звонок (`wbstream://...`, `https://stream.wb.ru/...`, `https://telemost.yandex.ru/j/...`, `dion://...`, `https://<portal>.bitrix24.ru/video/...`, `blink://...`, либо любая VK call-ссылка)
2. По желанию измените: имя в звонке, порт и логин/пароль SOCKS5 (прокси слушает только `127.0.0.1`), DNS для адаптера, режим туннеля (**Video (VP8)** или **DataChannel** - DC работает для VK, WB Stream, Bitrix и Blink), **Resources**, VP8 FPS / batch, **Dual-track** (VK, WB Stream, Bitrix, Blink) и **Reliable (KCP)** (все, кроме DION)
3. Поставьте/уберите галочку **SOCKS5 only (no system-wide routing)**:
   - Снято (по умолчанию) - поднимается TUN, весь трафик идёт через звонок (нужен root/admin)
   - Поставлено - только локальный SOCKS5, как на iOS
4. Нажмите **Start**
5. Дождитесь `TUNNEL CONNECTED` в логе - после этого SOCKS5 живёт на `127.0.0.1:<port>`, а с TUN-режимом весь трафик хоста идёт через звонок

При работе с VK впервые приложение откроет окно с капчей VK - решите её, после чего звонок начнёт собираться.

При первом подключении к Blink приложение покажет QR-код и ссылку для входа. Отсканируйте QR приложением Blink на телефоне, либо нажмите **Copy** и откройте ссылку на телефоне с Blink, затем подтвердите вход.

### Привилегии

| OS | Что требуется | Как получить |
|---|---|---|
| Windows | Administrator | Манифест `.exe` запрашивает UAC автоматически; согласитесь в диалоге |
| Linux | root для TUN-режима | При нажатии **Start** приложение само запросит пароль через `pkexec` - нужен polkit-агент, он есть в обычных десктопных окружениях. Без него: `xhost +SI:localuser:root` и затем `sudo -E ./WhitelistBypass.Joiner-*.AppImage --no-sandbox`. Без root доступен только режим **SOCKS5 only** |
| macOS | root для TUN-режима | `sudo "/Applications/WhitelistBypass Joiner.app/Contents/MacOS/WhitelistBypass Joiner"`. Без sudo доступен только режим **SOCKS5 only** |

### Когда выбирать десктоп вместо Android/iOS

- Нужен системный VPN на ноутбуке/PC
- Хочется GUI, а headless CLI ниже неудобно
- Нужен VK на компьютере (CLI-joiner-а для VK нет из-за капчи; десктоп GUI открывает окно с капчей сам)

## Joiner (Linux, headless)

Headless joiner для Linux / FreeBSD серверов, десктопов и роутеров. Поднимает локальный SOCKS5-прокси, через который можно пускать любой трафик (например `curl --socks5`, Telegram, system-wide через `redsocks`/`tun2socks`).

Скачайте архив под свою систему из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases), см. [Какой архив скачать](#какой-архив-скачать). В нём пять joiner-ов: `headless-wbstream-joiner`, `headless-telemost-joiner`, `headless-dion-joiner`, `headless-bitrix-joiner`, `headless-blink-joiner`.

> VK joiner не подходит под headless-подход: для входа в звонок нужно решать капчу, поэтому консольного бинарника для VK нет. Используйте Android, iOS или десктоп-приложение.

### Запуск

```sh
# WB Stream
./headless-wbstream-joiner --room wbstream://<uuid> --socks-port 1080

# Telemost
./headless-telemost-joiner --tm-link https://telemost.yandex.ru/j/<id> --socks-port 1080

# DION
./headless-dion-joiner --room dion://<id> --socks-port 1080

# Bitrix
./headless-bitrix-joiner --link https://<portal>.bitrix24.ru/video/<alias> --socks-port 1080

# Blink: сначала вход, потом подключение
./headless-blink-joiner --login --cookies cookies-blink-joiner.json
./headless-blink-joiner --room blink://<chat id> --cookies cookies-blink-joiner.json --socks-port 1080
```

Для Blink `--login` выводит ссылку - откройте её на телефоне с приложением Blink и подтвердите вход. Joiner должен входить отдельно от Creator, тем же файлом пользоваться нельзя. Файл перезаписывается при обновлении токена; если обновить не получилось, joiner завершится с ошибкой - снова запустите `--login`.

После строки `TUNNEL CONNECTED` SOCKS5 поднят на `127.0.0.1:<socks-port>`. Проверка:

```sh
curl --socks5 127.0.0.1:1080 https://api.ipify.org
```

### Флаги

| Флаг | WB | TM | DION | BX | BL | Описание |
|---|---|---|---|---|---|---|
| `--room <link>` | да | - | да | - | да | WB: `wbstream://<uuid>`, `https://stream.wb.ru/room/<uuid>` или UUID; DION: `dion://<id>` или `https://dion.vc/event/<id>`; Blink: `blink://<chat id>`, id чата или ссылка-приглашение в группу |
| `--tm-link <uri>` | - | да | - | - | - | `https://telemost.yandex.ru/j/<id>` |
| `--link <link>` | - | - | - | да | - | `https://<portal>.bitrix24.ru/video/<alias>` |
| `--cookies <path>` | - | - | - | - | да | файл со своим входом в Blink, например `cookies-blink-joiner.json` |
| `--login` | - | - | - | - | да | войти через приложение Blink, сохранить токены в `--cookies` и выйти |
| `--name <name>` | да | да | да | да | - | имя в звонке (по умолчанию `Joiner`) |
| `--socks-host <host>` | да | да | да | да | да | адрес прослушивания SOCKS5 (по умолчанию `127.0.0.1`; `0.0.0.0` чтобы выставить прокси на всех интерфейсах) |
| `--socks-port <port>` | да | да | да | да | да | порт SOCKS5 (по умолчанию `1080`) |
| `--socks-user <user>` | да | да | да | да | да | логин SOCKS5 (опционально) |
| `--socks-pass <pass>` | да | да | да | да | да | пароль SOCKS5 (опционально) |
| `--resources <mode>` | да | да | да | да | да | `default` / `moderate` / `unlimited` |
| `--tunnel-mode <mode>` | да | - | - | да | да | `video` или `dc` |
| `--vp8-fps <fps>` | да | да | - | да | да | частота VP8 кадров (по умолчанию `24`) |
| `--vp8-batch <n>` | да | да | - | да | да | множитель batch (по умолчанию `30`) |
| `--dual-track` | да | да | - | да | да | второй трек демонстрации экрана для режима video |
| `--reliable` | да | да | - | да | да | обернуть видеотуннель в KCP, см. [Что нужно](#что-нужно) |
| `--debug` | да | да | да | да | да | подробные логи |
| `--version` | да | да | да | да | да | Вывести версию и выйти |

При указании `--socks-user`/`--socks-pass` SOCKS5 требует аутентификацию. Без них прокси открыт без авторизации - на `127.0.0.1` (дефолт) это не страшно, но с `--socks-host 0.0.0.0` любой в локальной сети сможет использовать ваш канал. Всегда ставьте логин/пароль, если биндитесь на `0.0.0.0`.

### Системный туннель (как Android VPN)

Для проксирования всего трафика хоста используйте `tun2socks` или `redsocks` поверх локального SOCKS5. Пример с `tun2socks`:

```sh
sudo tun2socks -device tun://wb0 -proxy socks5://127.0.0.1:1080
```

## Если не работает

### Telegram работает, а сайты не открываются
- В системных настройках Android DNS должен быть "Автоматически", без Private DNS. Ручной вариант вроде `dns.google` ломает туннель
- В приложении выберите **Settings -> DNS -> Custom** (по умолчанию `8.8.8.8` / `8.8.4.4`). DNS оператора не подходит: под белыми списками он недоступен
- Проверьте адреса на опечатки: у Яндекса `77.88.8.8`, а не `77.8.8.8`

### Туннель работает минуту-другую и замирает, или скорость очень низкая
Похоже на ограничение скорости со стороны платформы или оператора, часто около 1,5 Мбит/с.
- Уменьшите **Batch** в **VP8 settings** до 3-5
- Включите **Reliable (KCP)**
- Попробуйте другой режим (DC или Video) и другую платформу
- Создайте звонок на Creator с другим провайдером: VK местами режет скорость звонков при белых списках

### В логе creator: `private destinations need --allow-private-dst`
Joiner обращается к адресу в локальной сети creator-а. Если это нужно, включите `--allow-private-dst` (в десктопном Creator - **Let the joiner reach my local network**), для `127.0.0.1` - `--allow-loopback`.

### iOS: после включения Happ или Streisand пропадает связь
VPN-приложение заворачивает в туннель и трафик самого звонка, получается петля. Добавьте в VPN-приложении прямой маршрут для адресов платформы - например, для DION список адресов есть на [faq.dion.vc](https://faq.dion.vc/ru/network_requirements).

### Windows 11 блокирует Creator
**Интеллектуальное управление приложениями** (Smart App Control) может блокировать Creator, иногда только со второго запуска. Отключите эту функцию.

### Бот: `/vk video` не работает
Это команда старого браузерного пути. Для headless используйте `/vk headless`, а режим туннеля выбирайте на joiner-е.

### Перенести десктопный Creator на другой компьютер
На Windows настройки и куки лежат в `%APPDATA%\WhitelistBypass Creator\`.

---

[Блог автора](https://t.me/markovdrankthechains)

### Поблагодарить автора

- `0xd986b7576340d8d7b04f806dfd38a182b19edf50` - USDC (ERC20)
- `TTEo4XXTB6CqhEiKpyoncfk3skEvoq3bCP` - USDT (TRC20)
- `UQCZge2JzLmwNtI56p-ZxLuN2f7ZMCPGqO0XTt3wJ-cy8JaE` - USDT (TON)
