# Running a headless creator unattended

`Restart=always` keeps the process alive. It does not keep the tunnel working.

The failure that actually costs you connectivity looks like this: the creator
is up, the room is joined, the log is still moving, new connections are still
being accepted - and nothing comes back. On the phone it reads as "connected,
no internet". systemd sees a healthy process, because it is one. The tunnel
underneath it is deaf.

This directory is what it takes to notice that and recover from it without
being at the keyboard.

| File | What it is |
|---|---|
| `creator-watchdog` | detects a deaf tunnel and restarts the instance |
| `creator-watchdog-replay` | runs the same rules over old logs, so you can check the thresholds before trusting them |
| `watchdog.conf.example` | thresholds and paths, all optional |
| `systemd/wbstream@.service` | templated creator unit, one instance per WB Stream account |
| `systemd/creator-watchdog@.{service,timer}` | runs the watchdog once a minute per instance |
| `logrotate/whitelist-bypass` | hourly rotation, with the `copytruncate` caveat spelled out |

## How a deaf tunnel is detected

The relay log carries the answer already. A connection that works logs
`first read <N>B` once payload comes back through it. A connection that opens
and then never reads anything logs `EOF with no data read` instead.

On a healthy tunnel the second line does not appear. Measured over 13.5 hours
of live traffic, including a night with the phone asleep and still opening
roughly 40 connections per 5 minutes, the count of empty connections was
exactly zero. That is what makes this workable: an empty connection is not a
rare-but-normal event to be filtered out, it is already the fault. The
thresholds only exist to decide how much evidence is enough.

Both counters at zero means nobody is connected - phone off, asleep, airplane
mode. The watchdog stays out of it.

## The three rules

**STALL** - no `first read` at all and at least 5 empty connections in 90 s.
This is the abrupt collapse, where every open connection dies at once. One
live incident put 107 empty connections into a single minute, against zero in
the preceding 25.

**QUIET** - no `first read` at all and at least 2 empty connections in 150 s.
Backstop for a tunnel that dies without the mass casualty event.

**DEGRADED** - at least 25 empty connections *and* more empty than reads in
120 s, with no requirement that reads drop to zero.

The third one took a second incident to find, and it is the interesting one.
Reads and empties came interleaved - 12 reads against 26 empties in one minute,
37 against 27 in the next. STALL never fired, because there were always reads
in its window. QUIET sat waiting for its window to drain of traffic that kept
trickling in. Recovery took eight minutes, and for most of those eight minutes
the tunnel was useless while the log looked busy.

Replaying the same log with DEGRADED in place trips it at 2 min 30 s.

Its threshold is deliberately the highest of the three. A short burst of empty
connections is normal when the client reconnects and the old connections get
reaped, and firing then would kick out the user who just came back.

## Install

Two instances shown, `main` and `spare`. One instance per WB Stream account -
a second creator on the same account fights the first one for the session.

```sh
sudo install -m 755 creator-watchdog creator-watchdog-replay /usr/local/bin/
sudo install -m 644 systemd/*.service systemd/*.timer /etc/systemd/system/
sudo install -m 644 logrotate/whitelist-bypass /etc/logrotate.d/
sudo mkdir -p /etc/whitelist-bypass /var/lib/whitelist-bypass

# one env file per instance, cookies exported from the desktop creator app
sudo cp systemd/wbstream@.env.example /etc/whitelist-bypass/wbstream-main.env
sudo cp watchdog.conf.example /etc/whitelist-bypass/watchdog-main.conf
sudoedit /etc/whitelist-bypass/wbstream-main.env
sudo chmod 600 /etc/whitelist-bypass/*.env /etc/whitelist-bypass/cookies-*.json

sudo systemctl daemon-reload
sudo systemctl enable --now wbstream@main creator-watchdog@main.timer
```

First start with `WB_FLAGS` empty, take the room UUID out of the link file,
put it into `WB_FLAGS` as `--room wbstream://<uuid>`, restart. From then on the
link survives restarts and you never retype it on the phone.

Check it came up:

```sh
journalctl -u wbstream@main -f          # expect: dc tunnel ready
journalctl -t creator-watchdog          # every restart it decided to make
```

## Calibrate before you trust it

The thresholds came from one tunnel. Yours will carry a different amount of
traffic, so replay them against your own history:

```sh
creator-watchdog-replay /var/log/wbstream-main.log*
```

Every line printed is a restart that would have happened. Walk them against
moments you remember losing connectivity. Lines you cannot account for are
false positives - raise the threshold for whichever rule named them and replay
again. On the reference tunnel a full day of logs yields exactly one line, on
the one incident that actually happened.

If it reports no empty connections at all, nothing here would ever fire, and
that is the correct outcome for a tunnel that has not failed yet. Keep the
watchdog installed anyway and replay again after your first outage - that log
is the calibration data.

## What this does not catch

**Expired cookies.** When the WB Stream refresh token dies, the creator never
gets into the room, so no connections are attempted and *both* counters sit at
zero - indistinguishable, to these rules, from a phone that is switched off.
The real signal is in the log as plain text, repeating every few seconds:

```
[auth] slide-v3 refresh: ... "unauthorized" ... "result":12
[session] start failed: ws dial: websocket: bad handshake (status 401)
```

Restarting does not fix it and the watchdog correctly refuses to try. Fresh
cookies have to be exported from the desktop creator app. If you want an alert
for this, match those two lines and notify - do not wire them to a restart.

**A client whose own state is wedged.** If the phone's DC state is the thing
that died, restarting the creator gets you a working room that the client still
cannot use. That is what `MAX_STREAK` is for: after three restarts that changed
nothing, the watchdog logs that the fault looks client-side and stops. Fix it
on the phone - fully kill the joiner app and reopen it, rather than toggling
reconnect.
