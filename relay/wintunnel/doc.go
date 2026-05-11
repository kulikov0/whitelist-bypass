// Package wintunnel brings up a wintun adapter on Windows, configures
// system-wide routing through it, and runs xjasonlyu/tun2socks as the
// engine that forwards every IP packet to a local SOCKS5 proxy.
//
// The package is the Windows counterpart to relay/androidbind. Android
// uses a VpnService fd plus addDisallowedApplication for the joiner's
// own traffic; on Windows there is no per-process exclusion, so the
// joiner's signaling and SFU media flows are kept off the tunnel by
// installing /32 bypass routes through the original default gateway.
//
// All real work is in wintunnel_windows.go; the !windows build is a
// stub so the relay module still compiles on macOS and Linux.
package wintunnel
