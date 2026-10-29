package netguard

import (
	"errors"
	"net/netip"
	"syscall"
)

var ErrBlocked = errors.New("blocked address")

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	cgnat,
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func IsLocal(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || cgnat.Contains(addr)
}

type PublicGuard struct {
	Allowed []netip.Prefix
}

func (g PublicGuard) Control(_, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil || !g.permits(addrPort.Addr().Unmap()) {
		return ErrBlocked
	}
	return nil
}

func (g PublicGuard) permits(addr netip.Addr) bool {
	for _, prefix := range g.Allowed {
		if prefix.Contains(addr) {
			return true
		}
	}
	return IsPublic(addr)
}
