// Package mdns is a minimal mDNS / DNS-SD responder. It advertises the
// _ezkobo._tcp service so phones can find the Kobo without an address, and
// answers A queries for <host>.local for browsers.
package mdns

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	svcType = "_ezkobo._tcp.local"
	svcEnum = "_services._dns-sd._udp.local"

	tA    = 1
	tPTR  = 12
	tTXT  = 16
	tAAAA = 28
	tSRV  = 33
	tNSEC = 47
	tANY  = 255
)

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// Service is what gets advertised.
type Service struct {
	Host     string // e.g. "kobo-1a2b.local"
	Instance string // e.g. "Libra Colour 1A2B"
	Port     int
	Model    string // shown by the app before it connects
}

func (s *Service) instFQDN() string { return s.Instance + "." + svcType }

// Run answers queries and announces svc until ctx is done, rejoining the
// network whenever the Kobo's address changes (Wi-Fi reconnects).
func Run(ctx context.Context, svc *Service) {
	for ctx.Err() == nil {
		// A session ends when our IP changes (WiFi reconnect) so we rejoin
		// the multicast group and re-announce on the new network.
		if err := mdnsSession(ctx, svc); err != nil {
			log.Printf("ezkobo: mdns: %v", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func mdnsSession(ctx context.Context, svc *Service) error {
	ips := LocalIPv4s()
	if len(ips) == 0 {
		return nil // no network yet
	}
	ip := net.ParseIP(ips[0])
	conn, err := listenMDNS(ctx, ip)
	if err != nil {
		return err
	}
	defer conn.Close()

	announce := func(ttl uint32) {
		msg := buildMessage(0, nil, svc.allRecords(ip, ttl, true), nil)
		conn.WriteToUDP(msg, mdnsGroup)
	}
	announce(120)
	time.AfterFunc(time.Second, func() { announce(120) })
	log.Printf("ezkobo: advertising %q on %s", svc.Instance, ip)

	buf := make([]byte, 9000)
	end := time.Now().Add(15 * time.Minute)
	for time.Now().Before(end) {
		if ctx.Err() != nil {
			announce(0) // goodbye: tell phones we're gone
			return nil
		}
		if !slices.Equal(ips, LocalIPv4s()) {
			return nil
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		id, qs, ok := parseQuery(buf[:n])
		if !ok {
			continue
		}
		myIP := localIPFor(src.IP)
		if myIP == nil {
			continue
		}
		// Queries not from port 5353 are "legacy unicast" and want a direct
		// reply that echoes the question.
		legacy := src.Port != 5353
		ans, extra := svc.answer(qs, myIP, !legacy)
		if len(ans) == 0 {
			continue
		}
		if legacy {
			conn.WriteToUDP(buildMessage(id, qs, ans, extra), src)
		} else {
			conn.WriteToUDP(buildMessage(0, nil, ans, extra), mdnsGroup)
		}
	}
	return nil
}

type question struct {
	name string
	typ  uint16
}

type record struct {
	name  string
	typ   uint16
	flush bool
	ttl   uint32
	data  []byte
}

func (s *Service) records(ip net.IP, ttl uint32, mcast bool) (ptr, srv, txt, a, enum record) {
	ptr = record{svcType, tPTR, false, ttl, encodeName(s.instFQDN())}
	srvData := binary.BigEndian.AppendUint16(nil, 0) // priority
	srvData = binary.BigEndian.AppendUint16(srvData, 0)
	srvData = binary.BigEndian.AppendUint16(srvData, uint16(s.Port))
	srv = record{s.instFQDN(), tSRV, mcast, ttl, append(srvData, encodeName(s.Host)...)}
	var txtData []byte
	for _, kv := range []string{"v=1", "ip=" + ip.String(), fmt.Sprintf("port=%d", s.Port), "model=" + s.Model} {
		txtData = append(append(txtData, byte(len(kv))), kv...)
	}
	txt = record{s.instFQDN(), tTXT, mcast, ttl, txtData}
	a = record{s.Host, tA, mcast, ttl, ip.To4()}
	enum = record{svcEnum, tPTR, false, ttl, encodeName(svcType)}
	return
}

func (s *Service) allRecords(ip net.IP, ttl uint32, mcast bool) []record {
	ptr, srv, txt, a, _ := s.records(ip, ttl, mcast)
	return []record{ptr, srv, txt, a}
}

// answer returns the answer and additional records for the questions we own.
func (s *Service) answer(qs []question, ip net.IP, mcast bool) (ans, extra []record) {
	ttl := uint32(120)
	if !mcast {
		ttl = 10 // RFC 6762 §6.7: legacy unicast replies use short TTLs
	}
	ptr, srv, txt, a, enum := s.records(ip, ttl, mcast)
	want := func(q question, t uint16) bool { return q.typ == t || q.typ == tANY }
	for _, q := range qs {
		switch {
		case strings.EqualFold(q.name, svcType) && want(q, tPTR):
			ans = append(ans, ptr)
			extra = append(extra, srv, txt, a)
		case strings.EqualFold(q.name, s.instFQDN()):
			if want(q, tSRV) {
				ans = append(ans, srv)
				extra = append(extra, a)
			}
			if want(q, tTXT) {
				ans = append(ans, txt)
			}
		case strings.EqualFold(q.name, s.Host) && want(q, tA):
			ans = append(ans, a)
		case strings.EqualFold(q.name, s.Host) && q.typ == tAAAA:
			// We have no IPv6 address. Say so (RFC 6762 §6.1), or browsers
			// wait ~5 s for an AAAA answer before using the A record.
			ans = append(ans, record{s.Host, tNSEC, mcast, ttl, nsecOnlyA(s.Host)})
			extra = append(extra, a)
		case strings.EqualFold(q.name, svcEnum) && want(q, tPTR):
			ans = append(ans, enum)
		}
	}
	return ans, extra
}

// parseQuery returns the questions of an mDNS query message.
func parseQuery(msg []byte) (uint16, []question, bool) {
	if len(msg) < 12 || msg[2]&0x80 != 0 {
		return 0, nil, false
	}
	id := binary.BigEndian.Uint16(msg[0:])
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	var qs []question
	off := 12
	for i := 0; i < qd; i++ {
		name, next, err := readName(msg, off)
		if err != nil || next+4 > len(msg) {
			return 0, nil, false
		}
		qs = append(qs, question{name, binary.BigEndian.Uint16(msg[next:])})
		off = next + 4
	}
	return id, qs, len(qs) > 0
}

func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	ret := -1
	for hops := 0; hops < 32; hops++ {
		if off >= len(msg) {
			break
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if ret < 0 {
				ret = off + 1
			}
			return strings.Join(labels, "."), ret, nil
		case l&0xC0 == 0xC0:
			if off+1 >= len(msg) {
				return "", 0, errors.New("bad pointer")
			}
			if ret < 0 {
				ret = off + 2
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
		default:
			if off+1+l > len(msg) {
				return "", 0, errors.New("bad label")
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
	return "", 0, errors.New("bad name")
}

// nsecOnlyA is NSEC record data asserting that name has only an A record:
// next name = name itself, then a type bitmap for window 0 with just A set.
func nsecOnlyA(name string) []byte {
	return append(encodeName(name), 0, 1, 0x40)
}

func encodeName(fqdn string) []byte {
	var b []byte
	for _, l := range strings.Split(fqdn, ".") {
		b = append(append(b, byte(len(l))), l...)
	}
	return append(b, 0)
}

// buildMessage builds a response. qs is echoed only for legacy unicast.
func buildMessage(id uint16, qs []question, ans, extra []record) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], id)
	binary.BigEndian.PutUint16(b[2:], 0x8400) // response, authoritative
	binary.BigEndian.PutUint16(b[4:], uint16(len(qs)))
	binary.BigEndian.PutUint16(b[6:], uint16(len(ans)))
	binary.BigEndian.PutUint16(b[10:], uint16(len(extra)))
	for _, q := range qs {
		b = append(b, encodeName(q.name)...)
		b = binary.BigEndian.AppendUint16(b, q.typ)
		b = binary.BigEndian.AppendUint16(b, 1)
	}
	for _, r := range append(ans, extra...) {
		class := uint16(1) // IN
		if r.flush {
			class |= 0x8000
		}
		b = append(b, encodeName(r.name)...)
		b = binary.BigEndian.AppendUint16(b, r.typ)
		b = binary.BigEndian.AppendUint16(b, class)
		b = binary.BigEndian.AppendUint32(b, r.ttl)
		b = binary.BigEndian.AppendUint16(b, uint16(len(r.data)))
		b = append(b, r.data...)
	}
	return b
}

// listenMDNS opens a socket on 0.0.0.0:5353 joined to the mDNS group on the
// interface with address ip. (net.ListenMulticastUDP binds to the group
// address itself, and replies sent from such a socket are ignored by Apple's
// mDNS stack.)
func listenMDNS(ctx context.Context, ip net.IP) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			if serr == nil {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
			}
		})
		if err != nil {
			return err
		}
		return serr
	}}
	pc, err := lc.ListenPacket(ctx, "udp4", "0.0.0.0:5353")
	if err != nil {
		return nil, err
	}
	conn := pc.(*net.UDPConn)
	raw, err := conn.SyscallConn()
	if err != nil {
		conn.Close()
		return nil, err
	}
	var ifaddr [4]byte
	copy(ifaddr[:], ip.To4())
	var serr error
	err = raw.Control(func(fd uintptr) {
		mreq := &syscall.IPMreq{Multiaddr: [4]byte{224, 0, 0, 251}, Interface: ifaddr}
		if serr = syscall.SetsockoptIPMreq(int(fd), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, mreq); serr != nil {
			return
		}
		if serr = syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, ifaddr); serr != nil {
			return
		}
		serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255)
	})
	if err == nil {
		err = serr
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// localIPFor picks our IPv4 address on the same subnet as peer.
func localIPFor(peer net.IP) net.IP {
	var fallback net.IP
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			if n.Contains(peer) {
				return n.IP
			}
			if fallback == nil {
				fallback = n.IP
			}
		}
	}
	return fallback
}

// LocalIPv4s returns the non-loopback IPv4 addresses of interfaces that are up.
func LocalIPv4s() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				out = append(out, n.IP.String())
			}
		}
	}
	return out
}
