package book

import (
	"archive/zip"
	"context"
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
	res := Process(context.Background(), Options{Kepub: true, CleanNames: true}, tmp,
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
	res := Process(context.Background(), Options{}, tmp, "emma.epub")
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

func TestProviderOrder(t *testing.T) {
	got := NormalizeProviders([]Provider{{"google", false}, {"bogus", true}, {"apple", true}, {"google", true}})
	ids := []string{}
	for _, p := range got {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != "google,apple,openlibrary,hardcover" || got[0].Enabled || got[3].Enabled {
		t.Fatalf("providers = %+v", got)
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
	st := Options{Kepub: true, Metadata: true, CleanNames: true, Providers: NormalizeProviders(nil)}
	res := Process(context.Background(), st, tmp, name)
	m, _ := readEPUBMeta(tmp)
	t.Logf("result %+v, title %q, author %q, cover %v", res, m.Title, m.Author, m.HasCover)
	if res.Metadata == "" || res.Metadata == "cleaned" {
		t.Fatalf("no online match: %+v", res)
	}
	if !m.HasCover {
		t.Fatal("no cover added")
	}
}
