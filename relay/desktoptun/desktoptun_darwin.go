//go:build darwin

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
	tunDev      string
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

	gw, iface, err := defaultIPv4GatewayDarwin()
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
	dev := t.cfg.AdapterName
	if !strings.HasPrefix(dev, "utun") {
		dev = "utun"
	}
	key := &engine.Key{
		Proxy:  proxy,
		Device: "tun://" + dev,
		MTU:    t.cfg.MTU,
	}
	t.log("[desktoptun] starting tun2socks engine adapter=%s mtu=%d proxy=socks5://%s:%d", dev, t.cfg.MTU, t.cfg.SocksHost, t.cfg.SocksPort)
	engine.Insert(key)
	engine.Start()

	actual, err := findUtunByMTU(t.cfg.MTU)
	if err != nil {
		engine.Stop()
		return fmt.Errorf("desktoptun: locate utun: %w", err)
	}
	t.tunDev = actual
	t.log("[desktoptun] kernel assigned %s", actual)

	if out, err := runCmd("ifconfig", actual, "inet", t.cfg.TunnelIP, t.cfg.TunnelPeer, "up"); err != nil {
		engine.Stop()
		return fmt.Errorf("desktoptun: ifconfig %s: %w (%s)", actual, err, out)
	}
	if out, err := runCmd("ifconfig", actual, "mtu", strconv.Itoa(t.cfg.MTU)); err != nil {
		t.log("[desktoptun] set mtu failed (continuing): %v (%s)", err, out)
	}

	for _, p := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if out, err := runCmd("route", "-n", "add", "-net", p, "-interface", actual); err != nil {
			t.log("[desktoptun] add default-half %s failed: %v (%s)", p, err, out)
		}
	}

	t.started = true
	t.log("[desktoptun] up: adapter=%s ip=%s peer=%s",
		actual, t.cfg.TunnelIP, t.cfg.TunnelPeer)
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
		if _, err := runCmd("route", "-n", "delete", "-host", ip); err != nil {
			t.log("[desktoptun] remove bypass route %s: %v", ip, err)
		}
	}
	t.bypass = nil

	for _, p := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if _, err := runCmd("route", "-n", "delete", "-net", p); err != nil {
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
	t.mu.Unlock()
	if gw == "" {
		return errors.New("desktoptun: original gateway unknown, start the tunnel first")
	}
	if _, err := runCmd("route", "-n", "add", "-host", addr, gw); err != nil {
		return fmt.Errorf("desktoptun: add bypass %s via %s: %w", addr, gw, err)
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		if _, err := runCmd("route", "-n", "delete", "-host", addr); err != nil {
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
	return exec.Command(name, args...).CombinedOutput()
}

func defaultIPv4GatewayDarwin() (gateway, iface string, err error) {
	out, nerr := exec.Command("netstat", "-rn", "-f", "inet").Output()
	if nerr != nil {
		return "", "", fmt.Errorf("netstat: %w", nerr)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[0] != "default" {
			continue
		}
		candidateGw := fields[1]
		candidateIf := fields[len(fields)-1]
		if net.ParseIP(candidateGw) == nil || net.ParseIP(candidateGw).To4() == nil {
			continue
		}
		return candidateGw, candidateIf, nil
	}
	return "", "", fmt.Errorf("no IPv4 default route found in netstat output:\n%s", string(out))
}

func findUtunByMTU(mtu int) (string, error) {
	out, err := exec.Command("ifconfig").Output()
	if err != nil {
		return "", err
	}
	var bestName string
	var bestNum = -1
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var current string
	var currentMTU int
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "utun") {
			if idx := strings.Index(line, ":"); idx > 0 {
				name := line[:idx]
				mtuPos := strings.Index(line, "mtu ")
				if mtuPos > 0 {
					currentMTU, _ = strconv.Atoi(strings.TrimSpace(line[mtuPos+4:]))
				}
				current = name
				if currentMTU == mtu {
					n, _ := strconv.Atoi(strings.TrimPrefix(name, "utun"))
					if n > bestNum {
						bestNum = n
						bestName = current
					}
				}
			}
		}
	}
	if bestName == "" {
		return "", errors.New("no utun with matching mtu")
	}
	return bestName, nil
}
