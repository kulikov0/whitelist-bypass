//go:build windows

package wintunnel

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

// Config describes one wintun adapter and the SOCKS5 endpoint that
// tun2socks should forward traffic to.
type Config struct {
	// AdapterName is the wintun adapter name shown in Windows. Must be unique.
	AdapterName string
	// TunnelIP is the local address assigned to the adapter (e.g. 10.99.0.2).
	TunnelIP string
	// TunnelMask is the subnet mask as dotted-quad (e.g. 255.255.255.0).
	TunnelMask string
	// TunnelPeer is the on-link next-hop used by the default routes
	// installed on the wintun adapter (e.g. 10.99.0.1). It does not need
	// to answer; wintun is point-to-point so Windows does not ARP for it.
	TunnelPeer string
	// MTU for the wintun adapter. 1500 is fine for most networks.
	MTU int
	// DNSServers is the list of resolvers to assign to the adapter.
	DNSServers []string
	// SocksHost / SocksPort / SocksUser / SocksPass: the local SOCKS5
	// proxy that the headless joiner is running. tun2socks dials this
	// for every captured TCP/UDP flow.
	SocksHost string
	SocksPort int
	SocksUser string
	SocksPass string
	LogFn     func(format string, args ...any)
}

// Tunnel owns one wintun adapter and the tun2socks engine instance
// driving it. Call New + Start to bring it up, Stop to tear it down.
type Tunnel struct {
	cfg Config
	log func(string, ...any)

	mu          sync.Mutex
	started     bool
	stopped     bool
	bypass      map[string]struct{} // /32 routes we installed for bypass
	origGateway string
	origIfAlias string
	tunIfIndex  uint32
}

// New validates a Config and returns a Tunnel ready to Start.
func New(cfg Config) (*Tunnel, error) {
	if cfg.AdapterName == "" {
		return nil, errors.New("wintunnel: AdapterName required")
	}
	if cfg.TunnelIP == "" || cfg.TunnelMask == "" || cfg.TunnelPeer == "" {
		return nil, errors.New("wintunnel: TunnelIP, TunnelMask and TunnelPeer required")
	}
	if cfg.MTU <= 0 {
		cfg.MTU = 1500
	}
	if cfg.SocksHost == "" {
		cfg.SocksHost = "127.0.0.1"
	}
	if cfg.SocksPort <= 0 {
		return nil, errors.New("wintunnel: SocksPort required")
	}
	if cfg.LogFn == nil {
		cfg.LogFn = log.Printf
	}
	if len(cfg.DNSServers) == 0 {
		cfg.DNSServers = []string{"1.1.1.1", "8.8.8.8"}
	}
	return &Tunnel{
		cfg:    cfg,
		log:    cfg.LogFn,
		bypass: make(map[string]struct{}),
	}, nil
}

// Start brings up the wintun adapter, captures the original default
// gateway so subsequent AddBypass* calls can pin /32 routes to it,
// configures the adapter, installs the split default route through
// the tunnel, and starts the tun2socks engine. After Start returns,
// every packet leaving the OS that does not match a bypass route
// will be steered through SOCKS5.
func (t *Tunnel) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return errors.New("wintunnel: already started")
	}

	gw, alias, err := defaultIPv4Gateway()
	if err != nil {
		return fmt.Errorf("wintunnel: read default gateway: %w", err)
	}
	t.origGateway = gw
	t.origIfAlias = alias
	t.log("[wintunnel] original default gateway %s via %q", gw, alias)

	proxy := fmt.Sprintf("socks5://%s:%d", t.cfg.SocksHost, t.cfg.SocksPort)
	if t.cfg.SocksUser != "" {
		proxy = fmt.Sprintf("socks5://%s:%s@%s:%d",
			t.cfg.SocksUser, t.cfg.SocksPass, t.cfg.SocksHost, t.cfg.SocksPort)
	}
	key := &engine.Key{
		Proxy:  proxy,
		Device: "tun://" + t.cfg.AdapterName,
		MTU:    t.cfg.MTU,
	}
	t.log("[wintunnel] starting tun2socks engine adapter=%s mtu=%d proxy=%s",
		t.cfg.AdapterName, t.cfg.MTU, proxy)
	engine.Insert(key)
	engine.Start()

	// adapter exists now; configure IP/MTU/DNS, install default routes
	if err := setAdapterIP(t.cfg.AdapterName, t.cfg.TunnelIP, t.cfg.TunnelMask); err != nil {
		engine.Stop()
		return fmt.Errorf("wintunnel: set adapter ip: %w", err)
	}
	if err := setAdapterMTU(t.cfg.AdapterName, t.cfg.MTU); err != nil {
		// non-fatal: log and continue
		t.log("[wintunnel] set mtu failed (continuing): %v", err)
	}
	if err := setAdapterDNS(t.cfg.AdapterName, t.cfg.DNSServers); err != nil {
		t.log("[wintunnel] set dns failed (continuing): %v", err)
	}
	idx, err := adapterIPv4Index(t.cfg.AdapterName)
	if err == nil {
		t.tunIfIndex = idx
	}

	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := addRouteViaAdapter(prefix, t.cfg.AdapterName, t.cfg.TunnelPeer, 2); err != nil {
			t.log("[wintunnel] add default-half %s failed: %v", prefix, err)
		}
	}

	t.started = true
	t.log("[wintunnel] up: adapter=%s ip=%s/%s peer=%s",
		t.cfg.AdapterName, t.cfg.TunnelIP, t.cfg.TunnelMask, t.cfg.TunnelPeer)
	return nil
}

// Stop removes every route we installed and shuts the wintun adapter
// down. Safe to call from multiple goroutines and idempotent.
func (t *Tunnel) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started || t.stopped {
		return
	}
	t.stopped = true

	for ip := range t.bypass {
		if err := deleteHostRoute(ip); err != nil {
			t.log("[wintunnel] remove bypass route %s: %v", ip, err)
		}
	}
	t.bypass = nil

	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := deleteRouteByPrefix(prefix, t.cfg.AdapterName); err != nil {
			t.log("[wintunnel] remove default-half %s: %v", prefix, err)
		}
	}

	engine.Stop()
	t.log("[wintunnel] down")
}

// AddBypassIP installs a /32 route for ip via the original default
// gateway. Used so the joiner's own connections to signaling and SFU
// hosts skip the tunnel and reach the real network directly.
func (t *Tunnel) AddBypassIP(ip net.IP) error {
	if ip == nil {
		return errors.New("wintunnel: nil IP")
	}
	v4 := ip.To4()
	if v4 == nil {
		// IPv6 bypass is out of scope: the joiner negotiates v4 hosts
		return nil
	}
	addr := v4.String()
	t.mu.Lock()
	if _, dup := t.bypass[addr]; dup {
		t.mu.Unlock()
		return nil
	}
	gw := t.origGateway
	t.mu.Unlock()
	if gw == "" {
		return errors.New("wintunnel: original gateway unknown, start the tunnel first")
	}
	if err := addHostRoute(addr, gw, 1); err != nil {
		return fmt.Errorf("wintunnel: add bypass %s via %s: %w", addr, gw, err)
	}
	t.mu.Lock()
	t.bypass[addr] = struct{}{}
	t.mu.Unlock()
	t.log("[wintunnel] bypass %s -> %s", addr, gw)
	return nil
}

// AddBypassHost resolves hostname (system resolver, no tunnel) and
// installs a /32 bypass route for every returned A record. The
// resolved IPs are returned so callers can wire them into other
// settings, e.g. a Pion ResolveICEHost callback.
func (t *Tunnel) AddBypassHost(hostname string) ([]net.IP, error) {
	if ip := net.ParseIP(hostname); ip != nil {
		return []net.IP{ip}, t.AddBypassIP(ip)
	}
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, fmt.Errorf("wintunnel: resolve %s: %w", hostname, err)
	}
	out := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			if addErr := t.AddBypassIP(v4); addErr != nil {
				t.log("[wintunnel] bypass host %s ip %s: %v", hostname, v4, addErr)
				continue
			}
			out = append(out, v4)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("wintunnel: no v4 addresses for %s", hostname)
	}
	return out, nil
}

// AddBypassFromCandidate parses an SDP candidate line ("candidate:..."
// or "a=candidate:...") and bypasses every IPv4 address found in it.
// Both the relay address and the rel-addr (TURN reflexive) are honored.
func (t *Tunnel) AddBypassFromCandidate(candidate string) error {
	ips := extractCandidateIPs(candidate)
	if len(ips) == 0 {
		return nil
	}
	var firstErr error
	for _, ip := range ips {
		if err := t.AddBypassIP(ip); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
