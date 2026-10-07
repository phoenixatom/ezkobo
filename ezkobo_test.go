package main

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	ok := map[string]string{
		"Dune.epub":                 "Dune.epub",
		"../../etc/Dune.epub":       "Dune.epub",
		`C:\Books\Dune.EPUB`:        "Dune.EPUB",
		`What? A "Book".kepub.epub`: "What_ A _Book_.kepub.epub",
		"Comic.cbz":                 "Comic.cbz",
	}
	for in, want := range ok {
		got, err := cleanName(in)
		if err != nil || got != want {
			t.Errorf("cleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "..", ".hidden.epub", "virus.sh", "notes", "book.epub/.."} {
		if got, err := cleanName(in); err == nil {
			t.Errorf("cleanName(%q) = %q; want error", in, got)
		}
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "A.kepub.epub")
	if got := uniquePath(p); got != p {
		t.Fatalf("got %q", got)
	}
	os.WriteFile(p, nil, 0o644)
	if got, want := uniquePath(p), filepath.Join(dir, "A (2).kepub.epub"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func query(id uint16, name string, typ uint16) []byte {
	return buildQuery(id, []question{{name, typ}})
}

func buildQuery(id uint16, qs []question) []byte {
	b := buildMessage(id, qs, nil, nil)
	b[2], b[3] = 0, 0 // flags: query
	return b
}

func TestMDNS(t *testing.T) {
	svc := &mdnsService{host: "ezkobo.local", instance: "Kobo 1A2B", port: 80}
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

func TestAppendLogRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ezkobo.log")
	l := &appendLog{path: path}
	line := []byte(strings.Repeat("x", 1023) + "\n")
	for i := 0; i < 1000; i++ { // ~1 MB written in total
		l.Write(line)
	}
	cur, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.Stat(path + ".old")
	if err != nil {
		t.Fatal(err)
	}
	if limit := int64(256<<10 + len(line)); cur.Size() > limit || old.Size() > limit {
		t.Fatalf("log not capped: current %d, old %d bytes", cur.Size(), old.Size())
	}
}

func TestAllBooksAndDeletePaths(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"Moby Dick.kepub.epub",                 // top level, like an existing library
		"Books/Emma.epub",                      // sent by EzKobo
		"Comics/Little Nemo/Little Nemo 1.cbz", // nested
		".kobo/KoboReader.sqlite",              // hidden: skipped
		".adds/koreader/manual.pdf",            // hidden: skipped
		"Moby Dick.kepub.sdr/metadata.lua",     // KOReader sidecar: skipped
		"fonts/Literata.ttf",                   // not a book
	} {
		p := filepath.Join(root, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	s := &server{cfg: config{library: root, dir: filepath.Join(root, "Books")}}
	books, err := s.allBooks()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range books {
		names = append(names, b.Name)
	}
	slices.Sort(names)
	want := []string{"Books/Emma.epub", "Comics/Little Nemo/Little Nemo 1.cbz", "Moby Dick.kepub.epub"}
	if !slices.Equal(names, want) {
		t.Fatalf("books = %v, want %v", names, want)
	}

	for in, want := range map[string]string{
		"Books/Emma.epub":       "Books/Emma.epub",
		"/Moby Dick.kepub.epub": "Moby Dick.kepub.epub",
		"../../etc/passwd.epub": "etc/passwd.epub", // clamped inside the root
	} {
		if got, err := cleanRelPath(in); err != nil || got != want {
			t.Errorf("cleanRelPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", ".kobo/KoboReader.sqlite", ".adds/koreader/manual.pdf", "fonts/Literata.ttf"} {
		if got, err := cleanRelPath(in); err == nil {
			t.Errorf("cleanRelPath(%q) = %q; want error", in, got)
		}
	}
}
