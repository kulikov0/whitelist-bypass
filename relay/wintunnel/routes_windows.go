//go:build windows

package wintunnel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// runHidden invokes a command with no console window. All wintunnel
// helpers use netsh / route / powershell here; subprocess parsing
// avoids pulling in winipcfg-style cgo or large Go bindings.
func runHidden(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	hideConsole(cmd)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("%s %s: %w (%s)",
			name, strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

// setAdapterIP gives the wintun adapter its tunnel IP.
func setAdapterIP(adapter, ipStr, mask string) error {
	_, err := runHidden("netsh", "interface", "ipv4", "set", "address",
		"name="+adapter, "static", ipStr, mask)
	return err
}

// setAdapterMTU sets the wintun adapter's MTU. netsh accepts this on
// any IPv4-capable interface, including wintun.
func setAdapterMTU(adapter string, mtu int) error {
	_, err := runHidden("netsh", "interface", "ipv4", "set", "subinterface",
		adapter, "mtu="+strconv.Itoa(mtu), "store=active")
	return err
}

// setAdapterDNS pins resolvers onto the wintun adapter. Windows picks
// resolvers per interface based on the routing decision, so this is
// what makes DNS lookups from non-bypassed apps go through the tunnel.
func setAdapterDNS(adapter string, servers []string) error {
	if len(servers) == 0 {
		return nil
	}
	if _, err := runHidden("netsh", "interface", "ipv4", "set", "dnsservers",
		"name="+adapter, "static", servers[0], "primary", "validate=no"); err != nil {
		return err
	}
	for _, s := range servers[1:] {
		if _, err := runHidden("netsh", "interface", "ipv4", "add", "dnsservers",
			"name="+adapter, s, "validate=no"); err != nil {
			return err
		}
	}
	return nil
}

// addRouteViaAdapter installs a prefix route bound to the wintun
// adapter. The nexthop must be on-link (typically TunnelPeer).
func addRouteViaAdapter(prefix, adapter, nexthop string, metric int) error {
	_, err := runHidden("netsh", "interface", "ipv4", "add", "route",
		"prefix="+prefix, "interface="+adapter,
		"nexthop="+nexthop, "metric="+strconv.Itoa(metric),
		"store=active")
	return err
}

func deleteRouteByPrefix(prefix, adapter string) error {
	_, err := runHidden("netsh", "interface", "ipv4", "delete", "route",
		"prefix="+prefix, "interface="+adapter)
	return err
}

// addHostRoute installs a /32 route for ip via the given gateway. Used
// for the joiner's signaling + SFU bypasses.
func addHostRoute(ip, gateway string, metric int) error {
	_, err := runHidden("route", "-p", "ADD", ip,
		"MASK", "255.255.255.255", gateway,
		"METRIC", strconv.Itoa(metric))
	if err == nil {
		return nil
	}
	// retry without persistence in case -p is rejected on this host
	_, err = runHidden("route", "ADD", ip,
		"MASK", "255.255.255.255", gateway,
		"METRIC", strconv.Itoa(metric))
	return err
}

func deleteHostRoute(ip string) error {
	_, err := runHidden("route", "DELETE", ip)
	return err
}

// adapterIPv4Index returns the InterfaceIndex of the wintun adapter
// once netsh has assigned an IP to it. Best-effort; used only for logs.
func adapterIPv4Index(adapter string) (uint32, error) {
	out, err := runHidden("powershell", "-NoProfile", "-Command",
		"(Get-NetAdapter -Name '"+adapter+"' -ErrorAction Stop).ifIndex")
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

// defaultIPv4Gateway picks the active default route, excluding any
// route that already points through our future wintun adapter. The
// returned alias is the InterfaceAlias of that route's interface,
// useful for logging.
type psRoute struct {
	NextHop        string `json:"NextHop"`
	InterfaceAlias string `json:"InterfaceAlias"`
	RouteMetric    int    `json:"RouteMetric"`
}

func defaultIPv4Gateway() (gateway, alias string, err error) {
	out, err := runHidden("powershell", "-NoProfile", "-Command",
		"Get-NetRoute -DestinationPrefix '0.0.0.0/0' -AddressFamily IPv4 -ErrorAction Stop "+
			"| Sort-Object -Property RouteMetric "+
			"| Select-Object NextHop,InterfaceAlias,RouteMetric "+
			"| ConvertTo-Json -Compress")
	if err != nil {
		return "", "", err
	}
	body := bytes.TrimSpace(out)
	if len(body) == 0 {
		return "", "", fmt.Errorf("no default route on this host")
	}
	if body[0] == '{' {
		body = append([]byte{'['}, append(body, ']')...)
	}
	var rows []psRoute
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", "", fmt.Errorf("parse Get-NetRoute output: %w", err)
	}
	for _, r := range rows {
		if r.NextHop == "" || r.NextHop == "0.0.0.0" {
			continue
		}
		if ip := net.ParseIP(r.NextHop); ip == nil {
			continue
		}
		return r.NextHop, r.InterfaceAlias, nil
	}
	return "", "", fmt.Errorf("no usable default route (out=%s)", string(out))
}
