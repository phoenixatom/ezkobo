package mdns

import (
	"net"
	"strings"
	"testing"
)

func query(id uint16, name string, typ uint16) []byte {
	return buildQuery(id, []question{{name, typ}})
}

func buildQuery(id uint16, qs []question) []byte {
	b := buildMessage(id, qs, nil, nil)
	b[2], b[3] = 0, 0 // flags: query
	return b
}

func TestMDNS(t *testing.T) {
	svc := &Service{Host: "ezkobo.local", Instance: "Kobo 1A2B", Port: 80}
	ip := net.IPv4(10, 0, 0, 7)

	id, qs, ok := parseQuery(query(0x1234, "_EzKobo._tcp.local", tPTR))
	if !ok || id != 0x1234 || len(qs) != 1 {
		t.Fatalf("parseQuery = %x %v %v", id, qs, ok)
	}
	ans, extra := svc.answer(qs, ip, true)
	if len(ans) != 1 || ans[0].typ != tPTR || len(extra) != 3 {
		t.Fatalf("PTR answer = %v / %v", ans, extra)
	}
	if name, _, _ := readName(ans[0].data, 0); name != "Kobo 1A2B._ezkobo._tcp.local" {
		t.Fatalf("PTR target = %q", name)
	}
	if txt := string(extra[1].data); !strings.Contains(txt, "ip=10.0.0.7") || !strings.Contains(txt, "port=80") {
		t.Fatalf("TXT = %q", txt)
	}

	// A response must not be parsed as a query.
	msg := buildMessage(0, nil, ans, extra)
	if _, _, ok := parseQuery(msg); ok {
		t.Fatal("response parsed as query")
	}

	_, qs, _ = parseQuery(query(1, "ezkobo.local", tA))
	if ans, _ := svc.answer(qs, ip, true); len(ans) != 1 || !net.IP(ans[0].data).Equal(ip) {
		t.Fatalf("A answer = %v", ans)
	}
	_, qs, _ = parseQuery(query(1, "ezkobo.local", tAAAA))
	if ans, extra := svc.answer(qs, ip, true); len(ans) != 1 || ans[0].typ != tNSEC || len(extra) != 1 {
		t.Fatalf("AAAA answer = %v / %v, want NSEC + A", ans, extra)
	}

	_, qs, _ = parseQuery(query(1, "printer.local", tA))
	if ans, _ := svc.answer(qs, ip, true); len(ans) != 0 {
		t.Fatalf("answered for another host: %v", ans)
	}
}
