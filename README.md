# Whitelist Bypass

Туннелирует интернет-трафик через платформы видеозвонков (VK Звонки, Яндекс Телемост) для обхода государственной цензуры на основе белых списков.

---

## Как это работает

Платформы видеозвонков используют WebRTC с SFU (Selective Forwarding Unit). SFU пробрасывает SCTP DataChannel между участниками не проверяя содержимое. Инструмент создаёт DataChannel рядом со штатными каналами звонка и использует его как двунаправленный туннель для данных.

Доступны два режима туннеля: **DC** (DataChannel) и **Pion Video** (кодирование данных в VP8-видеофреймы).

### Режим DC (DataChannel)

Браузерный режим. JavaScript-хуки перехватывают `RTCPeerConnection` на странице звонка, создают DataChannel и пробрасывают через него трафик.

- **VK Звонки** — Negotiated DataChannel id:2 (рядом с каналом анимодзи VK id:1). P2P через TURN relay
- **Телемост** — Non-negotiated DataChannel с меткой "sharing" (имитирует штатный трафик демонстрации экрана), SDP renegotiation через signaling WebSocket. SFU-архитектура

```
Joiner (Россия, Android)                  Creator (свободный интернет, ПК)

Все приложения
  |
VpnService (перехватывает весь трафик)
  |
tun2socks (IP → TCP)
  |
SOCKS5 прокси (Go, :1080)
  |
WebSocket (:9000)
  |
WebView (страница звонка)                 Electron (страница звонка)
  |                                         |
DataChannel  <------- TURN/SFU ------->  DataChannel
                                            |
                                        WebSocket (:9000)
                                            |
                                        Go relay
                                            |
                                        Интернет
```

### Режим Pion Video (VP8)

Go-режим. Библиотека Pion (Go WebRTC) подключается напрямую к TURN/SFU серверам платформы, минуя WebRTC-стек браузера. Данные кодируются внутри VP8 видеофреймов.

- **VK Звонки** — одно PeerConnection, P2P через TURN relay
- **Телемост** — двойное PeerConnection (pub/sub), SFU-архитектура

JS-хук заменяет `RTCPeerConnection` на `MockPeerConnection`, который пробрасывает все SDP/ICE операции к локальному Pion-серверу через WebSocket. Pion создаёт реальное PeerConnection с TURN серверами платформы.

**Кодирование данных в VP8:**
- Фреймы с данными: `[байт 0xFF][4 байта длины][payload]` — отправляются как VP8 video samples
- Keepalive фреймы: корректные VP8 interframe (17 байт) на 25fps, keyframe каждый 60-й фрейм — не дают SFU/TURN разорвать соединение
- Байт `0xFF` как маркер — не встречается в реальном VP8 (keyframe: bit0=0, interframe: bit0=1, поэтому `0xFF` не появляется)
- На приёмной стороне RTP-пакеты собираются в полные фреймы. Первый байт `0xFF` → данные; иначе → keepalive, игнорируется

**Протокол мультиплексирования** через VP8 туннель: `[4 байта длины фрейма][4 байта connID][1 байт msgType][payload]`
- Типы сообщений: Connect, ConnectOK, ConnectErr, Data, Close, UDP, UDPReply
- Несколько TCP/UDP соединений мультиплексируются в один VP8 видеопоток

```
Joiner (Россия, Android)                  Creator (свободный интернет, ПК)

Все приложения
  |
VpnService (перехватывает весь трафик)
  |
tun2socks (IP → TCP)
  |
SOCKS5 прокси (Go, :1080)
  |
VP8 туннель (Pion)                        VP8 туннель (Pion)
  |                                         |
MockPC (WebView)                          MockPC (Electron)
  |                                         |
Pion WebRTC  <------- TURN/SFU ------->  Pion WebRTC
                                            |
                                        Relay bridge
                                            |
                                        Интернет
```

Трафик проходит через TURN серверы платформы, которые находятся в белом списке. Для DPI-систем всё выглядит как обычный видеозвонок.

---

## Компоненты

### `hooks/` — JavaScript хуки

Внедряются в страницы звонков. Отдельные хуки для каждой платформы и роли:

| Файл | Назначение |
|---|---|
| `joiner-vk.js` | Joiner, VK Звонки, DC режим |
| `creator-vk.js` | Creator, VK Звонки, DC режим |
| `joiner-telemost.js` | Joiner, Телемост, DC режим |
| `creator-telemost.js` | Creator, Телемост, DC режим |
| `pion-vk.js` | VK Звонки, Pion Video режим (MockPeerConnection) |
| `pion-telemost.js` | Телемост, Pion Video режим (MockPeerConnection) |

- DC хуки перехватывают RTCPeerConnection, создают туннельный DataChannel, пробрасывают к локальному WebSocket
- Pion хуки заменяют RTCPeerConnection на MockPC, пробрасывают SDP/ICE к Pion через WebSocket
- Хуки для Телемоста включают фейковые медиа (камера/микрофон), чанкование сообщений (994 байта payload, 1000 байт всего), SDP renegotiation

### `relay/` — Go relay

| Путь | Назначение |
|---|---|
| `relay/mobile/` | DC режим: SOCKS5 прокси, WebSocket сервер, бинарный протокол фреймирования |
| `relay/pion/` | Pion Video режим: VP8 туннель, relay bridge, SOCKS5 прокси |
| `relay/pion/common.go` | Общие типы, WebSocket helper, парсинг ICE серверов |
| `relay/pion/vk.go` | VK Pion клиент (одно PeerConnection, P2P) |
| `relay/pion/telemost.go` | Телемост Pion клиент (двойное PeerConnection, pub/sub) |
| `relay/pion/vp8tunnel.go` | Кодирование/декодирование VP8 фреймов, генерация keepalive |
| `relay/pion/relay.go` | Relay bridge: мультиплексирование соединений, SOCKS5, UDP ASSOCIATE |
| `relay/mobile/tun_android.go` | Android: tun2socks + fdsan fix (CGo) |
| `relay/mobile/tun_stub.go` | Desktop заглушка (tun2socks не нужен) |

### `android-app/` — Android приложение (Joiner)

- WebView загружает страницу звонка и внедряет хуки (скрыт от пользователя — 1×1 пиксель)
- VpnService перехватывает весь трафик устройства
- Автоподключение: приложение само получает ссылку на звонок с link server и автоматически присоединяется
- Выбор режима туннеля: долгое нажатие кнопки Connect переключает DC ↔ Pion Video
- Отладочный лог: долгое нажатие на индикатор статуса показывает/скрывает лог
- Go relay подключён как `.aar` библиотека (gomobile), Pion relay — как нативный бинарь

### `creator-app/` — Electron приложение (Creator)

- WebView с постоянной сессией (cookies сохраняются между запусками)
- Снятие CSP заголовков для доступа к localhost WebSocket
- Автоматическое разрешение запросов камера/микрофон
- Выбор режима туннеля (DC / Pion Video)
- Go relay запускается как дочерний процесс
- Панели логов: relay и hook вывод

---

## Скачать

Готовые бинарники доступны в [GitHub Releases](../../releases).

---

## Установка и использование

### Вариант А: Ручной режим (без сервера)

Простейший вариант — creator вручную отправляет ссылку на звонок каждую сессию.

**Creator (свободный интернет, ПК)**

1. Скачай и установи Electron приложение из [GitHub Releases](../../releases)
2. Открой приложение
3. Выбери режим туннеля (DC или Pion Video)
4. Нажми «VK Call» или «Telemost»
5. Войди в аккаунт, создай звонок
6. Скопируй ссылку на звонок, отправь её Joiner-у (через любой мессенджер)

**Joiner (Россия, Android)**

1. Скачай и установи `whitelist-bypass.apk` из [GitHub Releases](../../releases)
2. Выбери режим туннеля (DC или Pion Video) — долгое нажатие на кнопку Connect
3. Вставь ссылку на звонок и нажми Connect
4. Приложение присоединится к звонку, установит туннель, запустит VPN
5. Весь трафик устройства идёт через звонок

---

### Вариант Б: Постоянный сервер (автоподключение)

Creator работает на VPS в headless-режиме 24/7. Android-приложение само получает ссылку на звонок — ничего отправлять вручную не нужно. WebView с VK-звонком скрыт от пользователя, видна только кнопка Connect.

**Архитектура**

```
VPS (свободный интернет)
  ├── creator-app (Electron + Xvfb)  — держит VK звонок открытым 24/7
  ├── link-server (Node.js :8080)    — отдаёт текущую ссылку на звонок по HTTP
  └── relay (Go :9000)               — пробрасывает DataChannel ↔ интернет

Android (Россия)
  └── App → GET http://VPS_IP:8080/link
        → загружает страницу звонка в скрытый WebView
        → автоматически нажимает "Войти"
        → устанавливает VPN
```

---

#### Шаг 1: VPS требования

Любой Linux VPS с:
- Публичным IP в сети со свободным интернетом (не за той же цензурой)
- Node.js 18+
- Xvfb (виртуальный дисплей для headless Electron)
- Открытые порты: **8080** (link server) и **9000** (relay WebSocket)

```sh
apt-get update && apt-get install -y xvfb nodejs npm
```

---

#### Шаг 2: Собрать VK сессию (cookies)

Creator должен быть авторизован во VКонтакте. Самый простой способ:

**Способ 1: Скопировать cookies из браузера**

1. Войди на [vk.com](https://vk.com) в Chrome или Firefox на любой машине
2. Открой DevTools (F12) → Application → Storage → Cookies → `https://vk.com`
3. Скопируй значения ключевых cookies:
   - `remixsid`
   - `remixsid_https`
   - `remixlang`
   - `remixlhk`

Эти cookies можно передать в Electron-сессию скриптом при первом запуске.

**Способ 2: Скопировать папку сессии Electron (рекомендуется)**

1. Установи creator-app на десктопе (macOS/Windows/Linux)
2. Открой приложение, нажми «VK Call», войди в аккаунт VK в открывшемся WebView
3. Закрой приложение
4. Скопируй папку сессии на VPS:

```sh
# macOS → Linux VPS
scp -r ~/Library/Application\ Support/whitelist-bypass-creator/ root@VPS_IP:/root/.config/whitelist-bypass-creator/

# Linux → Linux VPS
scp -r ~/.config/whitelist-bypass-creator/ root@VPS_IP:/root/.config/whitelist-bypass-creator/
```

После этого при запуске на VPS Electron автоматически возьмёт сохранённую сессию.

---

#### Шаг 3: Link server на VPS

Link server — маленький Node.js HTTP сервер. Creator через него публикует ссылку на текущий звонок, Android-приложение её забирает.

Сохрани файл `/opt/whitelist-bypass/link-server.js`:

```js
const http = require('http');
let currentLink = '';

http.createServer((req, res) => {
  res.setHeader('Access-Control-Allow-Origin', '*');

  if (req.method === 'POST' && req.url === '/link') {
    let body = '';
    req.on('data', chunk => body += chunk);
    req.on('end', () => {
      try {
        currentLink = JSON.parse(body).link;
        console.log('[link-server] Новая ссылка:', currentLink);
        res.end('ok');
      } catch (e) {
        res.writeHead(400);
        res.end('bad json');
      }
    });
  } else if (req.method === 'GET' && req.url === '/link') {
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ link: currentLink }));
  } else {
    res.writeHead(404);
    res.end();
  }
}).listen(8080, '0.0.0.0', () => {
  console.log('[link-server] Запущен на :8080');
});
```

---

#### Шаг 4: Запуск creator на VPS

```sh
# Скачай AppImage из Releases или собери самостоятельно (./build-creator.sh)
# Разместить в /opt/whitelist-bypass/

chmod +x /opt/whitelist-bypass/WhitelistBypass-linux.AppImage

# Запусти виртуальный дисплей
Xvfb :99 -screen 0 1280x800x24 &

# Запусти creator
DISPLAY=:99 /opt/whitelist-bypass/WhitelistBypass-linux.AppImage --no-sandbox &

# Запусти link server
node /opt/whitelist-bypass/link-server.js &
```

После запуска creator открывает Electron окно (невидимо, через Xvfb), открывает VK, создаёт звонок. После создания звонка нужно один раз опубликовать ссылку (подробнее ниже).

---

#### Шаг 5: Публикация ссылки звонка

После того как creator создал VK звонок, ссылку нужно отправить на link server. Это происходит автоматически через хук `creator-vk.js` — хук уже запущен в WebView creator-app и отправляет текущий `window.location.href` на `http://localhost:8080/link` при загрузке страницы звонка.

Если нужно отправить ссылку вручную — открой DevTools в creator-app (`Ctrl+Shift+I`) и выполни:

```js
fetch('http://localhost:8080/link', {
  method: 'POST',
  body: JSON.stringify({ link: window.location.href })
});
```

---

#### Шаг 6: Сборка Android APK с адресом твоего сервера

```sh
./build-app.sh -PlinkServerUrl=http://ВАШ_IP_VPS:8080/link
```

Или отредактируй `android-app/app/build.gradle.kts` перед сборкой:

```kotlin
val linkServerUrl = "http://ВАШ_IP_VPS:8080/link"
```

---

#### Шаг 7: Systemd сервисы (автозапуск на VPS)

Создай файлы сервисов:

**`/etc/systemd/system/vk-xvfb.service`**
```ini
[Unit]
Description=Xvfb virtual display for VK creator
After=network.target

[Service]
ExecStart=/usr/bin/Xvfb :99 -screen 0 1280x800x24
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

**`/etc/systemd/system/vk-creator.service`**
```ini
[Unit]
Description=VK call creator (headless Electron)
After=vk-xvfb.service network-online.target
Requires=vk-xvfb.service

[Service]
Environment=DISPLAY=:99
ExecStart=/opt/whitelist-bypass/WhitelistBypass-linux.AppImage --no-sandbox
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

**`/etc/systemd/system/vk-linkserver.service`**
```ini
[Unit]
Description=VK call link server
After=network.target

[Service]
ExecStart=/usr/bin/node /opt/whitelist-bypass/link-server.js
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

Включи и запусти:

```sh
systemctl daemon-reload
systemctl enable --now vk-xvfb vk-linkserver vk-creator

# Проверить статус
systemctl status vk-creator
journalctl -u vk-creator -f
```

---

## Сборка из исходников

### Требования

| Инструмент | Версия |
|---|---|
| Go | 1.21+ |
| gomobile | `go install golang.org/x/mobile/cmd/gomobile@latest` |
| gobind | `go install golang.org/x/mobile/cmd/gobind@latest` |
| Android SDK + NDK | NDK r29 |
| Java | 17+ |
| Node.js | 18+ |

### Скрипты сборки

```sh
# Собрать Go .aar и Pion relay для Android (копирует хуки)
./build-go.sh

# Собрать Android APK → prebuilts/whitelist-bypass.apk
./build-app.sh

# Собрать APK с адресом своего link server (Вариант Б)
./build-app.sh -PlinkServerUrl=http://ВАШ_IP:8080/link

# Собрать Electron приложения для всех платформ → prebuilts/
./build-creator.sh
```

Результаты в `prebuilts/`:

| Файл | Платформа |
|---|---|
| `WhitelistBypass Creator-*-arm64.dmg` | macOS (Apple Silicon) |
| `WhitelistBypass Creator-*-x64.exe` | Windows x64 |
| `WhitelistBypass Creator-*-ia32.exe` | Windows x86 |
| `WhitelistBypass Creator-*.AppImage` | Linux x64 |
| `whitelist-bypass.apk` | Android |

### Relay (отдельный бинарник)

```
relay --mode <режим> [--ws-port 9000] [--socks-port 1080]
```

| Режим | Описание |
|---|---|
| `joiner` | DC joiner: SOCKS5 :1080 + WebSocket :9000 |
| `creator` | DC creator: WebSocket :9000 → интернет |
| `vk-video-joiner` | Pion Video joiner для VK |
| `vk-video-creator` | Pion Video creator для VK |
| `telemost-video-joiner` | Pion Video joiner для Телемоста |
| `telemost-video-creator` | Pion Video creator для Телемоста |

### Кросс-компиляция relay

Go relay разбит на платформозависимые файлы:
- `relay/mobile/mobile.go` — общий сетевой код (SOCKS5, WebSocket, фреймирование)
- `relay/mobile/tun_android.go` — только Android: tun2socks + fdsan fix (CGo)
- `relay/mobile/tun_stub.go` — Desktop заглушка (tun2socks не нужен)

Это позволяет кросс-компилировать relay для macOS/Windows/Linux без CGo и Android NDK.

---

## Лицензия

[MIT](LICENSE)
