package webhooks

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/ribeirogab/zapgate/internal/netguard"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type BlockedAddressError struct {
	Host string
	Addr netip.Addr
}

func (e *BlockedAddressError) Error() string {
	return fmt.Sprintf("%s is not a loopback, private or CGNAT address, so the URL must use https", e.target())
}

func (e *BlockedAddressError) target() string {
	if e.Host == e.Addr.String() {
		return e.Host
	}
	return fmt.Sprintf("%s (%s)", e.Host, e.Addr)
}

type localDialer struct {
	resolver Resolver
	dialer   *net.Dialer
}

func (d localDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := lookup(ctx, d.resolver, host)
	if err != nil {
		return nil, err
	}
	if err := requireLocal(host, addrs); err != nil {
		return nil, err
	}
	return d.dialFirst(ctx, network, addrs, port)
}

func (d localDialer) dialFirst(ctx context.Context, network string, addrs []netip.Addr, port string) (net.Conn, error) {
	var first error
	for _, addr := range addrs {
		conn, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		if first == nil {
			first = err
		}
	}
	return nil, first
}

func lookup(ctx context.Context, resolver Resolver, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err == nil && len(addrs) == 0 {
		err = &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return addrs, err
}

func requireLocal(host string, addrs []netip.Addr) error {
	for _, addr := range addrs {
		if !netguard.IsLocal(addr) {
			return &BlockedAddressError{Host: host, Addr: addr.Unmap()}
		}
	}
	return nil
}
