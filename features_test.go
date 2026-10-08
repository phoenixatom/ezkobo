package main

import (
	"archive/zip"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeEPUB writes a minimal valid EPUB with the given title and author.
func makeEPUB(t *testing.T, path, title, author string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	w.Write([]byte("application/epub+zip"))
	w, _ = zw.Create("META-INF/container.xml")
	w.Write([]byte(`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`))
	w, _ = zw.Create("OEBPS/content.opf")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + title + `</dc:title>
    <dc:creator>` + author + `</dc:creator>
    <dc:identifier id="id">test</dc:identifier>
    <dc:language>en</dc:language>
  </metadata>
  <manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>`))
	w, _ = zw.Create("OEBPS/c1.xhtml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>c1</title></head><body><p>Call me Ishmael. Some years ago.</p></body></html>`))
	zw.Close()
	f.Close()
}

func TestProcessBookCleansConvertsAndRenames(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "upload.part")
	makeEPUB(t, tmp, "Moby Dick (Herman Melville) (example.org, mirror.net)", "Unknown")

	// Metadata lookup off: no network in tests.
	res := processBook(context.Background(), settings{Kepub: true, CleanNames: true}, tmp,
		"Moby Dick (Herman Melville) (example.org, mirror.net).epub")
	if res.Name != "Herman Melville - Moby Dick.kepub.epub" {
		t.Fatalf("name = %q", res.Name)
	}
	if !res.Converted || res.Metadata != "cleaned" {
		t.Fatalf("result = %+v", res)
	}
	m, err := readEPUBMeta(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Moby Dick" || m.Author != "Herman Melville" {
		t.Fatalf("metadata = %q / %q", m.Title, m.Author)
	}
	// kepubify marks converted text with koboSpan elements.
	zr, _ := zip.OpenReader(tmp)
	defer zr.Close()
	b, _ := readZipFile(&zr.Reader, "OEBPS/c1.xhtml")
	if !strings.Contains(string(b), "koboSpan") {
		t.Fatal("not converted to KEPUB")
	}
}

func TestProcessBookLeavesCleanBooksAlone(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "upload.part")
	makeEPUB(t, tmp, "Emma", "Jane Austen")
	res := processBook(context.Background(), settings{}, tmp, "emma.epub")
	if res.Name != "emma.epub" || res.Converted || res.Metadata != "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestJunkTags(t *testing.T) {
	for in, want := range map[string]string{
		"Butter (Asako Yuzuki) (example.org, mirror.net)": "Butter (Asako Yuzuki)",
		"Dune [site.com]":                  "Dune",
		"The Hobbit (Illustrated Edition)": "The Hobbit (Illustrated Edition)",
		"Vol. 2 (Part 1)":                  "Vol. 2 (Part 1)",
	} {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitTitleAuthor(t *testing.T) {
	for in, want := range map[string][2]string{
		"Butter (Asako Yuzuki, Polly Barton (translator))":                                            {"Butter", "Asako Yuzuki"},
		"The Restaurant of Lost Recipes (A Kamogawa Food Detectives Novel 2) (Hisashi Kashiwai etc.)": {"The Restaurant of Lost Recipes (A Kamogawa Food Detectives Novel 2)", "Hisashi Kashiwai"},
		"Moby Dick (Herman Melville)":                                                                 {"Moby Dick", "Herman Melville"},
		"Just A Title":                                                                                {"Just A Title", ""},
	} {
		if title, author := splitTitleAuthor(in); title != want[0] || author != want[1] {
			t.Errorf("splitTitleAuthor(%q) = %q, %q", in, title, author)
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

func TestKoboLibrary(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "lib")
	os.MkdirAll(filepath.Join(root, "Books"), 0o755)
	dbPath := filepath.Join(dir, "KoboReader.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE content (ContentID TEXT, ContentType INTEGER, Title TEXT, Attribution TEXT, ___PercentRead INTEGER)`)
	db.Exec(`INSERT INTO content VALUES
		('file://` + root + `/Books/Emma.kepub.epub', 6, 'Emma', 'Jane Austen', 42),
		('file://` + root + `/Books/Emma.kepub.epub!!c1', 9, 'Chapter 1', '', 0),
		('0f1e2d3c-store-book', 6, 'Store Book', 'Someone', 10)`)
	db.Close()

	s := &server{cfg: config{library: root, dir: filepath.Join(root, "Books"), db: dbPath}}
	lib := s.koboLibrary()
	e, ok := lib["Books/Emma.kepub.epub"]
	if !ok || e.Title != "Emma" || e.Author != "Jane Austen" || e.Progress != 42 || len(lib) != 1 {
		t.Fatalf("library = %+v", lib)
	}
}

// Live lookup against Google Books / Open Library. Run with EZKOBO_LIVE=1.
func TestLiveMetadataLookup(t *testing.T) {
	if os.Getenv("EZKOBO_LIVE") == "" {
		t.Skip("set EZKOBO_LIVE=1 to call Google Books / Open Library")
	}
	dir := t.TempDir()
	tmp := filepath.Join(dir, "upload.part")
	name := "Butter (Asako Yuzuki, Polly Barton (translator)) (example.org, mirror.net).epub"
	makeEPUB(t, tmp, strings.TrimSuffix(name, ".epub"), "")
	res := processBook(context.Background(), defaultSettings, tmp, name)
	m, _ := readEPUBMeta(tmp)
	t.Logf("result %+v, title %q, author %q, cover %v", res, m.Title, m.Author, m.HasCover)
	if res.Metadata != "google" && res.Metadata != "openlibrary" {
		t.Fatalf("no online match: %+v", res)
	}
	if !m.HasCover {
		t.Fatal("no cover added")
	}
}
