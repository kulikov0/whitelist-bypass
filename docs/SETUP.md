# Быстрый старт

Скачайте нужные файлы из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases).

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

## Что нужно

- **Creator** (сторона со свободным интернетом) - десктоп или headless на сервере
- **Joiner** (сторона с цензурой) - Android или iOS

Два режима туннеля: **DC** (DataChannel) и **Video** (VP8). Headless creator автоматически подстраивается под режим, выбранный joiner-ом, при каждом новом подключении. В legacy браузерном пути такого нет - нельзя создать DC в VK и подключиться по видео.

> **Рекомендуется headless с обеих сторон** - там есть обфускация траффика, настраиваемый VP8 pacing и WB Stream. Старый браузерный путь (Android `WebView` против Electron-creator с JS-хуками) ещё работает, но медленнее, без обфускации и постепенно выводится из эксплуатации.

## Creator (десктоп)

![Интерфейс](res/desktop_interface.png)

1. Скачайте и запустите приложение
2. Нажмите **+** для создания вкладки
3. Выберите сервис: **VK** или **Telemost**
4. Выберите тип подключения: **DC** (только VK) или **Video**
5. Авторизуйтесь в открывшемся окне
6. Создайте звонок
7. Скопируйте ссылку на звонок и отправьте на Joiner

**Headless режим** - создание звонка без браузера. Сначала авторизуйтесь через обычный режим (VK или Telemost), затем используйте **Headless VK** / **Headless TM**.

Если нужно запустить headless на сервере без GUI - экспортируйте куки кнопками **VK Cookies** / **Yandex Cookies** и используйте их с headless бинарниками (см. README).

> Запуск десктопного Creator на VPS без графического окружения через XPRA - см. [docs/vps/SETUP.md](vps/SETUP.md).

## Creator (headless, сервер)

Для запуска Creator на сервере без GUI.

### Подготовка кук

Куки нужны для авторизации на платформе. Экспортируйте их из десктопного Creator:

1. Откройте Creator на десктопе
2. Авторизуйтесь в VK или Telemost через обычный режим
3. Нажмите **VK Cookies** или **Yandex Cookies**
4. Скопируйте полученный файл на сервер

### Запуск

```sh
# VK
./headless-vk-creator --cookies cookies.json

# Telemost
./headless-telemost-creator --cookies cookies-yandex.json

# WB Stream (анонимный гостевой токен, куки не нужны)
./headless-wbstream-creator
```

После запуска Creator создаст звонок и выведет ссылку в лог. Ссылку нужно передать на Joiner.

### Подключение к существующему звонку

Чтобы не пересоздавать звонок при перезапуске, передайте существующую ссылку:

```sh
./headless-vk-creator       --cookies cookies.json        --vk-link https://vk.com/call/join/<token>
./headless-telemost-creator --cookies cookies-yandex.json --tm-link https://telemost.yandex.ru/j/<id>
./headless-wbstream-creator --room wbstream://<uuid>
```

### Флаги

| Флаг | VK | TM | WB | Описание |
|---|---|---|---|---|
| `--cookies <path>` | да | да | - | Путь к файлу с куками (JSON) |
| `--cookie-string <str>` | да | да | - | Куки строкой (`name=val; name=val`) |
| `--peer-id <id>` | да | - | - | VK peer_id для нового звонка |
| `--vk-link <link>` | да | - | - | Подключиться к существующему VK звонку |
| `--tm-link <uri>` | - | да | - | Подключиться к существующей Telemost конференции |
| `--room <id>` | - | - | да | Подключиться к существующей WB Stream комнате |
| `--resources <mode>` | да | да | да | `default` / `moderate` / `unlimited` / `custom` |
| `--read-buf <bytes>` | да | да | да | Размер read-буфера, только с `--resources custom` |
| `--max-dc-buf <bytes>` | да | - | - | Порог `BufferedAmountLowThreshold` DC, только с `--resources custom` |
| `--mem-limit <bytes>` | да | да | да | Soft memory limit Go рантайма, только с `--resources custom` |
| `--write-file <path>` | да | да | да | Файл, куда записывается активная ссылка на звонок |

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
  --cookies cookies.json \
  --vk-link https://vk.com/call/join/<token> \
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

- `/vk dc` - звонок VK, режим DC
- `/vk video` - звонок VK, режим Video
- `/vk headless` - звонок VK, headless режим
- `/tm video` - звонок Telemost, режим Video
- `/tm headless` - звонок Telemost, headless режим
- `/wb headless` - комната WB Stream
- `/list` - список активных вкладок
- `/close <id>` - закрыть вкладку по ID

![Команды бота](res/bot_commands.jpeg)

## Бот VK (headless, сервер)

Standalone Go-бинарник `headless-vk-bot` - то же самое, что бот в десктопном Creator, но без Electron. Слушает Long Poll сообщества, по команде запускает соответствующий headless-creator и присылает в чат join-link. Подходит для VPS: один процесс держит Long Poll и запускает creators по запросу.

### Подготовка

1. Создайте сообщество ВКонтакте и получите community access token, group ID и свой VK ID - порядок описан в разделе [Бот VK](#бот-vk) (шаги 1-8).
2. Скачайте бинарники из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases) и сложите их в одну папку - например `/usr/local/bin/`:
   - `headless-vk-bot-linux-x64`
   - `headless-vk-creator-linux-x64`
   - `headless-telemost-creator-linux-x64`
   - `headless-wbstream-creator-linux-x64`
   - `headless-dion-creator-linux-x64`

   Не забудьте дать права на выполнение: `chmod +x /usr/local/bin/headless-*`.
3. Подготовьте куки - все четыре платформы требуют залогиненную сессию. В десктопном Creator нажмите **Export Cookies** - получите `cookies.zip` со всеми четырьмя файлами (`cookies-vk.json`, `cookies-yandex.json`, `cookies-wbstream.json`, `cookies-dion.json`).
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
  --dion-cookies /etc/whitelist-bypass/cookies-dion.json
```

### Флаги

| Флаг | Описание |
|---|---|
| `--token <str>` | Community access token (обязательно) |
| `--group-id <id>` | ID сообщества, только цифры (обязательно) |
| `--user-id <ids>` | Список VK ID через запятую (`12345,67890`), которым разрешено отправлять команды. Пусто = разрешено всем (НЕ рекомендуется) |
| `--bins-dir <dir>` | Папка, где лежат `headless-vk-creator` / `headless-telemost-creator` / `headless-wbstream-creator` / `headless-dion-creator` (обязательно) |
| `--vk-cookies <path>` | Путь к VK куки (нужен для `/vk` и для join по VK call-ссылке) |
| `--tm-cookies <path>` | Путь к Yandex куки (нужен для `/tm` и для join по Telemost-ссылке) |
| `--wb-cookies <path>` | Путь к WB Stream куки (нужен для `/wb` и для join по `wbstream://` / `stream.wb.ru` ссылке) |
| `--dion-cookies <path>` | Путь к DION куки (нужен для `/dion` и для join по `dion://` / `dion.vc` ссылке) |
| `--sessions-dir <dir>` | Папка для логов запущенных creators. Опционально - без флага логи не пишутся, stdout/stderr creators отбрасываются |
| `--resources <mode>` | Режим ресурсов, передаётся каждому запускаемому creator: `default` / `moderate` / `unlimited`. По умолчанию `default`. `custom` не поддерживается, так как у каждого бинарника свой набор флагов настройки |

При получении команды бот запускает соответствующий creator с `--write-file <tmp>`, ждёт появления линка в файле (до 60 секунд) и присылает его в чат. `--user-id` принимает список через запятую; если указан, команды от пользователей не из списка игнорируются.

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
  --dion-cookies /etc/whitelist-bypass/cookies-dion.json
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

Альтернатива systemd. Готовый образ публикуется в GHCR (`ghcr.io/kulikov0/whitelist-bypass-bot`) - под капотом тот же `headless-vk-bot` плюс четыре creator-бинарника. Поддерживаемые архитектуры: `linux/amd64`, `linux/arm64`, `linux/386`.

```sh
mkdir wlb-bot && cd wlb-bot
curl -O https://raw.githubusercontent.com/kulikov0/whitelist-bypass/main/headless/docker/docker-compose.yml
curl -L https://raw.githubusercontent.com/kulikov0/whitelist-bypass/main/headless/docker/.env.example -o .env
# отредактируйте .env: VK_TOKEN, VK_GROUP_ID, VK_USER_IDS
# положите рядом cookies-vk.json, cookies-telemost.json, cookies-wbstream.json, cookies-dion.json
# (для платформ, которые не используете - создайте файл с содержимым `[]`)
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
| `TM_COOKIES` | нет | `/data/cookies-telemost.json` если есть | `--tm-cookies` |
| `WB_COOKIES` | нет | `/data/cookies-wbstream.json` если есть | `--wb-cookies` |
| `DION_COOKIES` | нет | `/data/cookies-dion.json` если есть | `--dion-cookies` |

> Если WebRTC-туннель не доходит через сетевой бридж Docker (UDP может отбрасываться), добавьте в `docker-compose.yml` строку `network_mode: host` под сервисом `bot`.

### Команды

- `/vk` - запустить `headless-vk-creator`
- `/tm` - запустить `headless-telemost-creator`
- `/wb` - запустить `headless-wbstream-creator`
- `/dion` - запустить `headless-dion-creator`
- `/list` - список активных сессий
- `/close <id>` - закрыть сессию по короткому ID
- `/start` - показать главное меню

Все команды также доступны через inline-клавиатуру в чате.

## Joiner (Android)

![Интерфейс](res/android_interface.png)

1. Скачайте и установите `whitelist-bypass.apk`
2. При первом запуске разрешите VPN-подключение в системном диалоге
3. Откройте настройки (кнопка справа от GO) и выберите режим туннеля (**DC** или **Video**). Headless-creator подстроится под выбранный режим автоматически
4. Вставьте ссылку на звонок в поле ввода
5. Нажмите **GO**
6. Дождитесь статуса "Tunnel active" - весь трафик устройства теперь идет через звонок

### Настройки

- **Tunnel** - режим туннеля (DC / Video)
- **Headless** - подключение без WebView (рекомендуется, включен по умолчанию)
- **Split tunneling** - выбор приложений, которые пойдут через туннель
- **Proxy settings** - порт SOCKS5, авторизация. Режим "Proxy only" - без VPN, только прокси
- **DNS settings** - системный или пользовательский DNS
- **VP8 pacing** - переопределить параметры VP8 пэйсинга (см. ниже)
- **Autoclick settings** - автоматический вход в звонок с указанным именем
- **Reconnect on app start** - автоподключение к последней ссылке при запуске
- **Show logs** - показать логи для отладки

### VP8 pacing

Управляет тем, как часто joiner отправляет VP8-кадры через SFU. Настраивается только на joiner; creator подстраивается под значения, переданные joiner-ом, в начале сессии.

- **Override VP8 pacing** - выключено по умолчанию. При выключенном чекбоксе используются дефолты `fps=24 batch=30` (≈6.5 Mbps теоретического потолка). При включении становятся доступны два поля.
- **FPS** - номинальная частота кадров VP8. Диапазон 1..240. Обычно 24-30.
- **Batch** - множитель плотности тиков. Реальная скорость отправки ≈ `fps × batch` кадров/сек. Диапазон 1..256.

Throughput ≈ `fps × batch × 1126 байт/кадр`. Примеры:

| fps | batch | потолок |
|----:|------:|--------:|
| 24  | 1     | ~27 KB/s |
| 24  | 8     | ~216 KB/s |
| 24  | 30    | ~810 KB/s (≈6.5 Mbps) |

Чем выше batch, тем больше нагрузка на CPU телефона и SFU. Если в логах появляются дропы пакетов или соединение нестабильно - уменьшите batch.

> **Важно:** обфускация и VP8 pacing работают только в режиме **headless-headless** - и creator, и joiner должны быть в headless-режиме.

### Если не работает

Попробуйте изменить DNS: в настройках приложения **DNS settings** выберите Custom (по умолчанию `8.8.8.8` / `8.8.4.4`). Также проверьте, что в системных настройках Android DNS выбрано "Автоматически" (без Private DNS).

## Joiner (iOS)

На iOS доступен только режим SOCKS5 прокси (без VPN). Для проксирования всего трафика устройства используйте VPN-приложение с поддержкой SOCKS5 (например Happ, Shadowrocket или Streisand).

1. Скачайте `whitelist-bypass-proxy.ipa` (unsigned) из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases) и установите его. Подробная инструкция по установке unsigned IPA на iPhone: [habr.com/ru/articles/1013990/](https://habr.com/ru/articles/1013990/). Либо соберите из исходников (см. README).
2. Установите любое VPN-приложение с поддержкой SOCKS5 (Happ, Shadowrocket, Streisand и т.п.).
3. Откройте whitelist-bypass, выберите режим туннеля (**DC** или **Video**), вставьте ссылку на звонок и нажмите **Go**. Headless-creator подстроится под выбранный режим автоматически.
4. Дождитесь статуса "Tunnel Active". Приложение покажет адрес SOCKS5 прокси (например `socks5://user:pass@127.0.0.1:1081`).
5. Скопируйте параметры SOCKS5 из whitelist-bypass.
6. Вставьте их в VPN-приложение и подключитесь - весь трафик устройства пойдет через туннель.

### Подключение через Happ

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

### Настройки

- **Tunnel mode** - режим туннеля (DC / Video)
- **Auth mode** - авторизация прокси (Auto - случайные креденшалы, Manual - свои)
- **Display name** - имя при входе в звонок
- **VP8 Pacing** - переопределить параметры VP8 (см. раздел VP8 pacing выше)
- **Show logs** - показать логи для отладки

> Режим SOCKS5 прокси выбран из-за ограничений Apple: использование NetworkExtension (VPN) требует платного Apple Developer аккаунта и не работает через sideload. Если кто-то из комьюнити реализует полноценный VPN на основе этих исходников - будет круто.

## Joiner (Десктоп: Windows / macOS / Linux)

GUI joiner для десктопа. Поддерживает все три платформы: **VK**, **Telemost** и **WB Stream**. Автоматически определяет платформу по ссылке.

![Интерфейс](res/joiner_desktop_interface.jpeg)

В отличие от Linux headless joiner ниже, десктоп-joiner может поднять системный VPN-туннель: TUN-адаптер плюс split-default route, через который выходит весь трафик хоста (как Android VPN-режим). На Windows используется wintun, на Linux - `/dev/net/tun` + `iproute2`, на macOS - utun + `route(8)`/`ifconfig`. Если TUN не нужен - есть галочка **SOCKS5 only**, поднимается только локальный прокси.

### Скачать

Из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases):

- `WhitelistBypass Joiner-<version>-x64.exe` / `-ia32.exe` - Windows (portable, без установщика)
- `WhitelistBypass Joiner-<version>-x86_64.AppImage` - Linux
- `WhitelistBypass Joiner-<version>-arm64.dmg` / `-x64.dmg` - macOS

### Запуск

1. Вставьте ссылку на звонок (`wbstream://...`, `https://stream.wb.ru/...`, `https://telemost.yandex.ru/j/...`, либо любая VK call-ссылка)
2. По желанию измените: имя в звонке, порт SOCKS5, логин/пароль SOCKS5, режим туннеля (**Video** или **DataChannel** - доступен только для VK), VP8 FPS / batch, DNS для адаптера
3. Поставьте/уберите галочку **SOCKS5 only**:
   - Снято (по умолчанию) - поднимается TUN, весь трафик идёт через звонок (нужен root/admin)
   - Поставлено - только локальный SOCKS5, как на iOS
4. Нажмите **Start**
5. Дождитесь `TUNNEL CONNECTED` в логе - после этого SOCKS5 живёт на `127.0.0.1:<port>`, а с TUN-режимом весь трафик хоста идёт через звонок

При работе с VK впервые приложение откроет окно с капчей VK - решите её, после чего звонок начнёт собираться.

### Привилегии

| OS | Что требуется | Как получить |
|---|---|---|
| Windows | Administrator | Манифест `.exe` запрашивает UAC автоматически; согласитесь в диалоге |
| Linux | root для TUN-режима | Запустите `xhost +SI:localuser:root` и затем `sudo -E ./WhitelistBypass\ Joiner-*.AppImage --no-sandbox`. Без root доступен только режим **SOCKS5 only** |
| macOS | root для TUN-режима | `sudo "/Applications/WhitelistBypass Joiner.app/Contents/MacOS/WhitelistBypass Joiner"`. Без sudo доступен только режим **SOCKS5 only** |

### Когда выбирать десктоп вместо Android/iOS

- Нужен системный VPN на ноутбуке/PC
- Хочется GUI, а headless CLI ниже неудобно
- Нужен VK (headless CLI VK не поддерживает из-за капчи; десктоп GUI открывает окно с капчей сам)

## Joiner (Linux, headless)

Headless joiner для Linux-серверов и десктопов. Поднимает локальный SOCKS5-прокси, через который можно пускать любой трафик (например `curl --socks5`, Telegram, system-wide через `redsocks`/`tun2socks`).

Скачайте бинарник из [GitHub Releases](https://github.com/kulikov0/whitelist-bypass/releases):

- `headless-wbstream-joiner-linux-x64` / `-ia32` - для WB Stream
- `headless-telemost-joiner-linux-x64` / `-ia32` - для Telemost

> VK joiner не подходит под headless-подход: для входа в звонок нужно решать капчу, поэтому Linux-бинарника для VK нет. Используйте Android/iOS клиент.

### Запуск

```sh
# WB Stream
./headless-wbstream-joiner --room wbstream://<uuid> --socks-port 1080

# Telemost
./headless-telemost-joiner --tm-link https://telemost.yandex.ru/j/<id> --socks-port 1080
```

После строки `TUNNEL CONNECTED` SOCKS5 поднят на `127.0.0.1:<socks-port>`. Проверка:

```sh
curl --socks5 127.0.0.1:1080 https://api.ipify.org
```

### Флаги

| Флаг | WB | TM | Описание |
|---|---|---|---|
| `--room <link>` | да | - | `wbstream://<uuid>` или просто UUID комнаты |
| `--tm-link <uri>` | - | да | `https://telemost.yandex.ru/j/<id>` |
| `--name <name>` | да | да | имя в звонке (по умолчанию `Joiner`) |
| `--socks-port <port>` | да | да | порт SOCKS5 (по умолчанию `1080`) |
| `--socks-user <user>` | да | да | логин SOCKS5 (опционально) |
| `--socks-pass <pass>` | да | да | пароль SOCKS5 (опционально) |
| `--resources <mode>` | да | да | `default` / `moderate` / `unlimited` |
| `--vp8-fps <fps>` | да | да | частота VP8 кадров (по умолчанию `24`) |
| `--vp8-batch <n>` | да | да | множитель batch (по умолчанию `30`) |

При указании `--socks-user`/`--socks-pass` SOCKS5 требует аутентификацию. Без них прокси открыт для всех на `127.0.0.1`.

### Системный туннель (как Android VPN)

Для проксирования всего трафика хоста используйте `tun2socks` или `redsocks` поверх локального SOCKS5. Пример с `tun2socks`:

```sh
sudo tun2socks -device tun://wb0 -proxy socks5://127.0.0.1:1080
```

---

[Блог автора](https://t.me/markovdrankthechains)

### Поблагодарить автора

- `0xd986b7576340d8d7b04f806dfd38a182b19edf50` - USDC (ERC20)
- `TTEo4XXTB6CqhEiKpyoncfk3skEvoq3bCP` - USDT (TRC20)
