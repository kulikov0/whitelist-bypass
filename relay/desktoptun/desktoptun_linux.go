//go:build linux

package desktoptun

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/xjasonlyu/tun2socks/v2/engine"

	"whitelist-bypass/relay/common"
)

type Config struct {
	AdapterName string
	TunnelIP    string
	TunnelMask  string
	TunnelPeer  string
	MTU         int
	DNSServers  []string
	SocksHost   string
	SocksPort   int
	SocksUser   string
	SocksPass   string
	LogFn       func(format string, args ...any)
}

type Tunnel struct {
	cfg Config
	log func(string, ...any)

	mu          sync.Mutex
	started     bool
	stopped     bool
	bypass      map[string]struct{}
	origGateway string
	origIface   string
}

func New(cfg Config) (*Tunnel, error) {
	if cfg.AdapterName == "" {
		return nil, errors.New("desktoptun: AdapterName required")
	}
	if cfg.TunnelIP == "" || cfg.TunnelMask == "" || cfg.TunnelPeer == "" {
		return nil, errors.New("desktoptun: TunnelIP, TunnelMask and TunnelPeer required")
	}
	if cfg.MTU <= 0 {
		cfg.MTU = 1500
	}
	if cfg.SocksHost == "" {
		cfg.SocksHost = common.SocksLocalhostIP
	}
	if cfg.SocksPort <= 0 {
		return nil, errors.New("desktoptun: SocksPort required")
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

func (t *Tunnel) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return errors.New("desktoptun: already started")
	}

	gw, iface, err := defaultIPv4GatewayLinux()
	if err != nil {
		return fmt.Errorf("desktoptun: read default gateway: %w", err)
	}
	t.origGateway = gw
	t.origIface = iface
	t.log("[desktoptun] original default gateway %s via %q", gw, iface)

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
	t.log("[desktoptun] starting tun2socks engine adapter=%s mtu=%d proxy=socks5://%s:%d",
		t.cfg.AdapterName, t.cfg.MTU, t.cfg.SocksHost, t.cfg.SocksPort)
	engine.Insert(key)
	engine.Start()

	prefix, err := maskToCIDR(t.cfg.TunnelMask)
	if err != nil {
		engine.Stop()
		return fmt.Errorf("desktoptun: bad mask: %w", err)
	}
	addr := fmt.Sprintf("%s/%d", t.cfg.TunnelIP, prefix)
	if out, err := runCmd("ip", "addr", "add", addr, "dev", t.cfg.AdapterName); err != nil {
		engine.Stop()
		return fmt.Errorf("desktoptun: ip addr add %s: %w (%s)", addr, err, out)
	}
	if out, err := runCmd("ip", "link", "set", t.cfg.AdapterName, "mtu", strconv.Itoa(t.cfg.MTU)); err != nil {
		t.log("[desktoptun] set mtu failed (continuing): %v (%s)", err, out)
	}
	if out, err := runCmd("ip", "link", "set", t.cfg.AdapterName, "up"); err != nil {
		engine.Stop()
		return fmt.Errorf("desktoptun: ip link set up: %w (%s)", err, out)
	}

	for _, p := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if out, err := runCmd("ip", "route", "add", p, "dev", t.cfg.AdapterName); err != nil {
			t.log("[desktoptun] add default-half %s failed: %v (%s)", p, err, out)
		}
	}

	t.started = true
	t.log("[desktoptun] up: adapter=%s ip=%s peer=%s",
		t.cfg.AdapterName, addr, t.cfg.TunnelPeer)
	return nil
}

func (t *Tunnel) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started || t.stopped {
		return
	}
	t.stopped = true

	for ip := range t.bypass {
		if _, err := runCmd("ip", "route", "del", ip+"/32"); err != nil {
			t.log("[desktoptun] remove bypass route %s: %v", ip, err)
		}
	}
	t.bypass = nil

	for _, p := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if _, err := runCmd("ip", "route", "del", p, "dev", t.cfg.AdapterName); err != nil {
			t.log("[desktoptun] remove default-half %s: %v", p, err)
		}
	}

	engine.Stop()
	t.log("[desktoptun] down")
}

func (t *Tunnel) AddBypassIP(ip net.IP) error {
	if ip == nil {
		return errors.New("desktoptun: nil IP")
	}
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	addr := v4.String()
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return nil
	}
	if _, dup := t.bypass[addr]; dup {
		t.mu.Unlock()
		return nil
	}
	gw := t.origGateway
	iface := t.origIface
	t.mu.Unlock()
	if gw == "" {
		return errors.New("desktoptun: original gateway unknown, start the tunnel first")
	}
	if _, err := runCmd("ip", "route", "add", addr+"/32", "via", gw, "dev", iface); err != nil {
		return fmt.Errorf("desktoptun: add bypass %s via %s: %w", addr, gw, err)
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		if _, err := runCmd("ip", "route", "del", addr+"/32"); err != nil {
			t.log("[desktoptun] remove orphaned bypass %s: %v", addr, err)
		}
		return nil
	}
	t.bypass[addr] = struct{}{}
	t.mu.Unlock()
	t.log("[desktoptun] bypass %s -> %s", addr, gw)
	return nil
}

func (t *Tunnel) AddBypassHost(hostname string) ([]net.IP, error) {
	if ip := net.ParseIP(hostname); ip != nil {
		return []net.IP{ip}, t.AddBypassIP(ip)
	}
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, fmt.Errorf("desktoptun: resolve %s: %w", hostname, err)
	}
	out := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			if addErr := t.AddBypassIP(v4); addErr != nil {
				t.log("[desktoptun] bypass host %s ip %s: %v", hostname, v4, addErr)
				continue
			}
			out = append(out, v4)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("desktoptun: no v4 addresses for %s", hostname)
	}
	return out, nil
}

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

func runCmd(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

func defaultIPv4GatewayLinux() (gateway, iface string, err error) {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return "", "", fmt.Errorf("ip route show default: %w", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		var gw, dev string
		for i := 0; i < len(fields)-1; i++ {
			switch fields[i] {
			case "via":
				gw = fields[i+1]
			case "dev":
				dev = fields[i+1]
			}
		}
		if gw != "" && dev != "" {
			return gw, dev, nil
		}
	}
	return "", "", errors.New("no IPv4 default route found")
}

func maskToCIDR(mask string) (int, error) {
	parts := strings.Split(mask, ".")
	if len(parts) != 4 {
		return 0, fmt.Errorf("bad mask %q", mask)
	}
	var bits int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return 0, fmt.Errorf("bad mask octet %q", p)
		}
		for i := 7; i >= 0; i-- {
			if n&(1<<i) != 0 {
				bits++
			}
		}
	}
	return bits, nil
}
