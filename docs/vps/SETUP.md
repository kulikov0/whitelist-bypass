# Подготовка VPS 

## Требования
Проверялось на 
- 1 vCPU
- 1 GB RAM

## XPRA

Для установки whitelist-bypass требуется настроить графическое окружение. 

У `Creator` есть `headless` версия, но она сырая, так что ставим обычную с GUI. Так как на нашем VPS 90% нет графического окружения, то используем [XPRA](https://github.com/Xpra-org/xpra/wiki/Download#-linux)

```shell
curl https://xpra.org/get-xpra.sh | bash
```

# Установка

Скачиваем `Creator` для `Linux` из [Гитхаб релизов](https://github.com/kulikov0/whitelist-bypass/releases/)


## Получаем ссылку на актуальный файл `Creator`
- Заходим на страницу релизов, находим последний, у него будет написано `latest` (*цифра `1` на скриншоте*)
- Разворачиваем список ассетов, нажимаем на `Assets` (*цифра `2` на скрине*)

![alt](./assets/image/open-latest-release-assets.jpg)

## Получаем ссылку на свежий `Creator` 
- Находим в списке ассетов из предыдущего шага файл с `.AppImage` на конце. Это наше. 
- Нажимаем по нему `ПКМ` (*правая кнопка мыши, место клика отмечено цифрой `1` на скриншоте) 
- Выбираем пункт `Копировать ссылку` (*Пункт `Копировать ссылку` отмечен цифрой `2` на скриншоте*)

![alt](./assets/image/copy-latest-creator-direct-link.jpg)


## Устанавливаем зависимости

Так как `Creator` будет разворачиваться через AppImage, то нужно установить зависимости для его запуска:

```shell
sudo add-apt-repository universe
sudo apt install libfuse2
```

## Скачиваем `Creator` на сервер

Скачиваем приложение по ссылке, полученной ранее
```shell
wget https://github.com/kulikov0/whitelist-bypass/releases/download/v0.4.0/WhitelistBypass.Creator-0.4.0-x86_64.AppImage && \
mv *.AppImage creator.AppImage
```

Выдаем права на выполнение нашему скачанному приложению (он сохранен под именем `creator.AppImage`!)
```shell
chmod +x creator.AppImage
```

Перемещаем его в `/bin` директорию
```shell
sudo mv creator.AppImage /usr/bin/whitelist-bypass-creator
```

## Управляющие скрипты

Добавляем скрипт для остановки `Creator`
```shell
sudo tee /usr/bin/whitelist-bypass-stop > /dev/null << 'EOF'
#!/usr/bin/env bash
xpra stop 100
EOF
```

Добавляем скрипт для запуска `Creator`
```shell
sudo tee /usr/bin/whitelist-bypass-start > /dev/null << 'EOF'
#!/usr/bin/env bash
xpra start :100 --pulseaudio=no --webcam=no --mdns=no --resize-display=1200x900 --attach=yes --daemon=no --html=on --bind-tcp=127.0.0.1:10000 --start='xterm -e whitelist-bypass-creator --no-sandbox'
EOF
```

## Systemd (автозагрузка)

> Можно пропустить, если не требуется автозапуск при рестарте VPS. 

Создаем сервис
```shell
sudo tee /etc/systemd/system/whitelist-bypass-start.service > /dev/null << 'EOF'
[Unit]
Description=Whitelist Bypass Creator Service
Documentation=https://github.com/kulikov0/whitelist-bypass/
After=xpra-server.service

[Service]
Type=simple
ExecStart=bash /usr/bin/whitelist-bypass-start

[Install]
WantedBy=multi-user.target
EOF
```

Выполняем команды:
```shell
sudo systemctl daemon-reload
sudo systemctl enable whitelist-bypass-start.service
sudo systemctl start whitelist-bypass-start.service
```

Перезагружаемся. 
> После перезагрузки VPS иногда долго поднимаются, не боимся!
```shell
sudo reboot
```

# Подключение

> Если не делали автостарт `Creator` при перезагрузке VPS, то может потребоваться подключиться к VPS и выполнить команду `whitelist-bypass-start`

После установки можем подключаться к нашему `Creator` и настраивать его согласно обычной инструкции. 

## Проброс XPRA порта
Открываем терминал, пишем команду
```shell
ssh vps-username@vps-ip -NL 10000:localhost:10000
```

Этот терминал не закрываем до конца работы с `Creator` 

## Окно подключения

После проброса порта можно подключаться. 

Топаем в любимом браузере по ссылке http://localhost:10000/connect.html

## Настройки подключения

- В открывшемся окне открываем `Advanced Options`

![alt](./assets/image/open-xpra-advanced-settings.jpg)

- Там находим пункт `Keyboard Layout` (*цифра `1` на скриншоте*) и выставляем его в `English USA` (*цифра `2` на скриншоте*)

![alt](./assets/image/choose-xpra-keyboard-layout.jpg)

- Нажимем зеленую кнопку `Connect`
- После загрузки и подключения видим, что где-то в углу нас ждет окошко `Creator`!  