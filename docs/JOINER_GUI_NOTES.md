# Desktop Joiner state and settings

The desktop GUI stores its last successfully launched settings in `settings.json`
inside Electron's application user-data directory. The conference link is stored
locally; the SOCKS password is intentionally not persisted. Treat that settings
file as private and do not include it in bug reports.

The GUI refuses to start if its local SOCKS port is occupied. It does not stop
the process using that port. Close the other client or select a different port.
This is a preflight check; the backend still owns the final socket bind.

Reopening the window restores the current status and recent log. Fields are
disabled while the backend is running. Both `TUNNEL CONNECTED` and
`TUNNEL_CONNECTED` status formats are recognized. Closing the application
cancels a pending reconnect.

VP8 batch values up to 3000 can be entered; allowing a value is not a
recommendation to use it. The default remains 30. Existing default SOCKS port,
DNS, TUN and reliable-mode settings are preserved.

## Validation

From `joiner-desktop-app`, install dependencies and run:

```sh
npm run build
node --test test/settings.test.cjs test/lifecycle.test.cjs
```

The tests cover argument validation, Telemost/WB Stream/Bitrix KCP flags,
occupied-port handling, cancellation during startup, status recognition,
password omission and reconnect cancellation. Electron lifecycle tests use
mock processes and do not start a tunnel or modify routes.

## Separate tuning experiments

The local deployment that motivated these GUI fixes also tested KCP window
1024 and a server pending-send limit of 1536 instead of 2048. A short ABA series
of 5 MB single-stream downloads produced medians of 14.82 s (2048), 8.89 s
(1536), and 13.79 s (2048). This was not a controlled throughput benchmark:
network conditions varied and only one 1536 series completed successfully.
The 2048 configuration was restored after testing.

Those transport experiments and their custom binaries are not part of this
GUI patch. These results do not establish an optimal window, pending limit,
or performance improvement for other networks. Private deployment addresses,
conference links, credentials and support logs are not included.
