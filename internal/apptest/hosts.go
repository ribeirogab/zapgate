package apptest

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"sync"
)

type Hosts struct {
	mu    sync.Mutex
	addrs map[string][]netip.Addr
}

func newHosts() *Hosts {
	return &Hosts{addrs: map[string][]netip.Addr{}}
}

func (h *Hosts) Point(name string, addrs ...string) {
	parsed := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		parsed = append(parsed, netip.MustParseAddr(addr))
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.addrs[name] = parsed
}

func (h *Hosts) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	addrs := h.addrs[host]
	if len(addrs) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return slices.Clone(addrs), nil
}
