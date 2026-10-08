package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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

func TestPIN(t *testing.T) {
	s := &server{cfg: config{stateDir: t.TempDir()}}
	h := s.requirePIN(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	call := func(pin string) int {
		r := httptest.NewRequest("GET", "/api/books", nil)
		if pin != "" {
			r.Header.Set("X-EzKobo-PIN", pin)
		}
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}
	if c := call(""); c != 200 {
		t.Fatalf("no PIN set: %d", c)
	}
	os.WriteFile(s.pinPath(), []byte("4821\n"), 0o600)
	if c := call(""); c != 401 {
		t.Fatalf("missing PIN: %d", c)
	}
	if c := call("4821"); c != 200 {
		t.Fatalf("right PIN: %d", c)
	}
	for i := 0; i < 5; i++ {
		call("0000")
	}
	if c := call("4821"); c != 429 {
		t.Fatalf("after 5 wrong PINs even the right one should wait: %d", c)
	}
}

func TestSettingsKeepGoogleKeySecret(t *testing.T) {
	s := &server{cfg: config{stateDir: t.TempDir()}}
	put := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.putSettings(w, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body)))
		return w
	}
	if w := put(`{"googleApiKey":"AIzaSyExampleKey123"}`); w.Code != 200 || strings.Contains(w.Body.String(), "AIza") {
		t.Fatalf("put key: %d %s", w.Code, w.Body)
	}
	if st := s.loadSettings(); st.GoogleAPIKey != "AIzaSyExampleKey123" || !st.Kepub {
		t.Fatalf("stored settings = %+v", st)
	}
	// Changing another setting leaves the key alone.
	put(`{"kepub":false}`)
	if st := s.loadSettings(); st.GoogleAPIKey == "" || st.Kepub {
		t.Fatalf("after toggling kepub: %+v", st)
	}
	w := httptest.NewRecorder()
	s.getSettings(w, httptest.NewRequest("GET", "/api/settings", nil))
	if strings.Contains(w.Body.String(), "AIza") || !strings.Contains(w.Body.String(), `"googleApiKeySet":true`) {
		t.Fatalf("get leaked key or missing flag: %s", w.Body)
	}
	put(`{"googleApiKey":""}`)
	if s.loadSettings().GoogleAPIKey != "" {
		t.Fatal("key not removed")
	}
	if w := put(`{"googleApiKey":"bad key/with?stuff"}`); w.Code != 400 {
		t.Fatalf("bad key accepted: %d", w.Code)
	}
}
