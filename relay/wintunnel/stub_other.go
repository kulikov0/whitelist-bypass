//go:build !windows

package wintunnel

import (
	"errors"
	"net"
)

var errUnsupported = errors.New("wintunnel: only supported on windows")

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

type Tunnel struct{}

func New(_ Config) (*Tunnel, error)                  { return nil, errUnsupported }
func (*Tunnel) Start() error                         { return errUnsupported }
func (*Tunnel) Stop()                                {}
func (*Tunnel) AddBypassIP(_ net.IP) error           { return errUnsupported }
func (*Tunnel) AddBypassHost(_ string) ([]net.IP, error) {
	return nil, errUnsupported
}
func (*Tunnel) AddBypassFromCandidate(_ string) error { return errUnsupported }
