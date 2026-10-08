package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/beevik/etree"
	"github.com/pgaskin/kepubify/v4/kepub"
	_ "golang.org/x/crypto/x509roots/fallback" // CA roots if the Kobo has none
)

// processed describes what happened to an uploaded book.
type processed struct {
	Name      string `json:"name"`      // final file name
	Converted bool   `json:"converted"` // EPUB → KEPUB
	Metadata  string `json:"metadata"`  // "", "cleaned", "google", "openlibrary"
}

// processBook applies the Kobo's settings to an uploaded file at tmp, which
// was sent as name. It may rewrite tmp in place and returns the final name.
func processBook(ctx context.Context, st settings, tmp, name string) processed {
	res := processed{Name: name}
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".epub") {
		return res // only EPUBs carry metadata we can fix or convert
	}
	isKepub := strings.HasSuffix(lower, ".kepub.epub")

	meta, err := readEPUBMeta(tmp)
	if err != nil {
		log.Printf("ezkobo: %s: reading metadata: %v", name, err)
		return res
	}

	if st.Metadata || st.CleanNames {
		if fixed, source := fixMetadata(ctx, meta, name, st.Metadata); fixed != nil {
			if err := writeEPUBMeta(tmp, fixed); err != nil {
				log.Printf("ezkobo: %s: writing metadata: %v", name, err)
			} else {
				meta, res.Metadata = fixed, source
			}
		}
	}

	if st.Kepub && !isKepub {
		if err := convertKepub(ctx, tmp); err != nil {
			log.Printf("ezkobo: %s: KEPUB conversion failed, keeping EPUB: %v", name, err)
		} else {
			res.Converted, isKepub = true, true
		}
	}

	ext := ".epub"
	if isKepub {
		ext = ".kepub.epub"
	}
	base := strings.TrimSuffix(strings.TrimSuffix(name, filepath.Ext(name)), ".kepub")
	if st.CleanNames && meta.Title != "" {
		base = meta.Title
		if meta.Author != "" {
			base = meta.Author + " - " + meta.Title
		}
	}
	if n, err := cleanName(base + ext); err == nil {
		res.Name = n
	}
	return res
}

// --- EPUB metadata -----------------------------------------------------------

type epubMeta struct {
	Title    string
	Author   string
	HasCover bool
	Cover    []byte // a new cover to add, if any
}

// readEPUBMeta reads title, author and whether a cover exists from the OPF.
func readEPUBMeta(file string) (*epubMeta, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	opfPath, doc, err := readOPF(&zr.Reader)
	if err != nil {
		return nil, err
	}
	_ = opfPath
	m := &epubMeta{}
	if md := doc.FindElement("//metadata"); md != nil {
		if e := md.FindElement("title"); e != nil {
			m.Title = strings.TrimSpace(e.Text())
		}
		if e := md.FindElement("creator"); e != nil {
			m.Author = strings.TrimSpace(e.Text())
		}
		for _, e := range md.SelectElements("meta") {
			if e.SelectAttrValue("name", "") == "cover" {
				m.HasCover = true
			}
		}
	}
	for _, e := range doc.FindElements("//manifest/item") {
		if strings.Contains(e.SelectAttrValue("properties", ""), "cover-image") {
			m.HasCover = true
		}
	}
	return m, nil
}

func readOPF(zr *zip.Reader) (string, *etree.Document, error) {
	container, err := readZipFile(zr, "META-INF/container.xml")
	if err != nil {
		return "", nil, err
	}
	cdoc := etree.NewDocument()
	if err := cdoc.ReadFromBytes(container); err != nil {
		return "", nil, err
	}
	rf := cdoc.FindElement("//rootfile")
	if rf == nil {
		return "", nil, errors.New("no rootfile in container.xml")
	}
	opfPath := rf.SelectAttrValue("full-path", "")
	opf, err := readZipFile(zr, opfPath)
	if err != nil {
		return "", nil, err
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(opf); err != nil {
		return "", nil, err
	}
	return opfPath, doc, nil
}

func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 8<<20))
}

// writeEPUBMeta rewrites the EPUB at file with m's title, author and cover.
func writeEPUBMeta(file string, m *epubMeta) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()
	opfPath, doc, err := readOPF(&zr.Reader)
	if err != nil {
		return err
	}
	md := doc.FindElement("//metadata")
	if md == nil {
		return errors.New("no metadata in OPF")
	}
	setDC(md, "title", m.Title)
	setDC(md, "creator", m.Author)

	coverPath := ""
	if m.Cover != nil && !m.HasCover {
		if manifest := doc.FindElement("//manifest"); manifest != nil {
			item := manifest.CreateElement("item")
			item.CreateAttr("id", "ezkobo-cover")
			item.CreateAttr("href", "ezkobo-cover.jpg")
			item.CreateAttr("media-type", "image/jpeg")
			item.CreateAttr("properties", "cover-image") // EPUB 3
			meta := md.CreateElement("meta")             // EPUB 2
			meta.CreateAttr("name", "cover")
			meta.CreateAttr("content", "ezkobo-cover")
			coverPath = path.Join(path.Dir(opfPath), "ezkobo-cover.jpg")
		}
	}
	doc.Indent(2)
	opf, err := doc.WriteToBytes()
	if err != nil {
		return err
	}

	out, err := os.CreateTemp(filepath.Dir(file), ".ezkobo-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	zw := zip.NewWriter(out)
	for _, f := range zr.File {
		if f.Name == opfPath {
			w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
			if err != nil {
				return err
			}
			w.Write(opf)
			continue
		}
		if err := zw.Copy(f); err != nil { // keeps "mimetype" first and stored
			return err
		}
	}
	if coverPath != "" {
		w, err := zw.Create(coverPath)
		if err != nil {
			return err
		}
		w.Write(m.Cover)
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	zr.Close()
	return os.Rename(out.Name(), file)
}

// setDC sets the text of the first dc:<name> element, creating it if needed.
func setDC(md *etree.Element, name, value string) {
	if value == "" {
		return
	}
	e := md.FindElement(name)
	if e == nil {
		e = md.CreateElement("dc:" + name)
	}
	e.SetText(value)
}

// --- Cleaning and lookup ------------------------------------------------------

// junk matches a bracketed website tag that download sites append to titles
// and file names, e.g. "(example.org, mirror.net)" or "[site.com]".
var junk = regexp.MustCompile(`(?i)\s*[(\[][^()\[\]]*\b[a-z0-9-]+\.[a-z]{2,6}\b[^()\[\]]*[)\]]`)

func isMessy(s string) bool {
	return s == "" || strings.EqualFold(s, "unknown") || junk.MatchString(s) || strings.Contains(s, "_")
}

func cleanText(s string) string {
	s = junk.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "_", " ")
	return strings.Join(strings.Fields(s), " ")
}

// fixMetadata returns improved metadata, or nil if nothing changed. With
// online set, it looks the book up (Google Books, then Open Library) when
// the title or author is missing or messy, or the book has no cover.
func fixMetadata(ctx context.Context, m *epubMeta, fileName string, online bool) (*epubMeta, string) {
	fixed := *m
	source := ""

	// Without metadata, fall back to the file name, which is often
	// "Title (Author) (site.tld).epub".
	if isMessy(fixed.Title) {
		src := cleanText(m.Title)
		if src == "" {
			src = cleanText(strings.TrimSuffix(strings.TrimSuffix(fileName, filepath.Ext(fileName)), ".kepub"))
		}
		fixed.Title = src
		// "Title (Author)": only split when there's no real author, so titles
		// like "The Hobbit (Illustrated Edition)" stay intact.
		if isMessy(fixed.Author) {
			if title, author := splitTitleAuthor(src); author != "" {
				fixed.Title, fixed.Author = title, author
			}
		}
	}
	fixed.Title = cleanText(fixed.Title)
	fixed.Author = cleanText(fixed.Author)
	if fixed.Title != m.Title || fixed.Author != m.Author {
		source = "cleaned"
	}

	needsLookup := isMessy(m.Title) || isMessy(m.Author) || !m.HasCover
	if online && needsLookup && fixed.Title != "" {
		lctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if r, src := lookupBook(lctx, fixed.Title, fixed.Author); r != nil {
			// Covers redirect a few times and can take several seconds.
			cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			if isMessy(m.Title) && r.Title != "" {
				fixed.Title = r.Title
			}
			if isMessy(m.Author) && r.Author != "" {
				fixed.Author = r.Author
			}
			if !m.HasCover && r.CoverURL != "" {
				fixed.Cover = fetchCover(cctx, r.CoverURL)
			}
			source = src
		}
	}
	if source == "" && fixed.Cover == nil {
		return nil, ""
	}
	return &fixed, source
}

// splitTitleAuthor splits "Title (Author)" into its parts, handling nested
// brackets like "Butter (Asako Yuzuki, Polly Barton (translator))". Only the
// first author is kept.
func splitTitleAuthor(s string) (title, author string) {
	if !strings.HasSuffix(s, ")") {
		return s, ""
	}
	depth := 0
	for i := len(s) - 1; i > 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			depth--
		}
		if depth == 0 {
			title = strings.TrimSpace(s[:i])
			author = s[i+1 : len(s)-1]
			break
		}
	}
	if title == "" {
		return s, ""
	}
	author, _, _ = strings.Cut(author, ",")
	author, _, _ = strings.Cut(author, " (")
	author = strings.TrimSuffix(strings.TrimSpace(author), " etc.")
	return title, strings.TrimSpace(author)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

type bookInfo struct {
	Title, Author, CoverURL string
}

var httpClient = &http.Client{Timeout: 12 * time.Second}

const userAgent = "EzKobo (+https://github.com/phoenixatom/ezkobo)"

// lookupBook tries Open Library first: it needs no key and has no shared
// quota. Keyless Google Books requests share a public daily quota that is
// often used up, so Google is only a fallback.
func lookupBook(ctx context.Context, title, author string) (*bookInfo, string) {
	if b := lookupOpenLibrary(ctx, title, author); b != nil {
		return b, "openlibrary"
	}
	if b := lookupGoogle(ctx, title, author); b != nil {
		return b, "google"
	}
	return nil, ""
}

func lookupGoogle(ctx context.Context, title, author string) *bookInfo {
	q := "intitle:" + title
	if author != "" {
		q += " inauthor:" + author
	}
	var res struct {
		Items []struct {
			VolumeInfo struct {
				Title      string   `json:"title"`
				Subtitle   string   `json:"subtitle"`
				Authors    []string `json:"authors"`
				ImageLinks struct {
					Thumbnail string `json:"thumbnail"`
				} `json:"imageLinks"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	if getJSON(ctx, "https://www.googleapis.com/books/v1/volumes?maxResults=5&q="+url.QueryEscape(q), &res) != nil {
		return nil
	}
	for _, it := range res.Items {
		v := it.VolumeInfo
		if !sameTitle(title, v.Title) {
			continue
		}
		b := &bookInfo{Title: v.Title}
		if len(v.Authors) > 0 {
			b.Author = v.Authors[0]
		}
		if t := v.ImageLinks.Thumbnail; t != "" {
			// Ask for a larger image than the default thumbnail.
			t = strings.Replace(strings.Replace(t, "http://", "https://", 1), "&edge=curl", "", 1)
			b.CoverURL = strings.Replace(t, "zoom=1", "zoom=3", 1)
		}
		return b
	}
	return nil
}

func lookupOpenLibrary(ctx context.Context, title, author string) *bookInfo {
	v := url.Values{"title": {title}, "limit": {"5"}, "fields": {"title,author_name,cover_i"}}
	if author != "" {
		v.Set("author", author)
	}
	var res struct {
		Docs []struct {
			Title   string   `json:"title"`
			Authors []string `json:"author_name"`
			Cover   int      `json:"cover_i"`
		} `json:"docs"`
	}
	if getJSON(ctx, "https://openlibrary.org/search.json?"+v.Encode(), &res) != nil {
		return nil
	}
	for _, d := range res.Docs {
		if !sameTitle(title, d.Title) {
			continue
		}
		b := &bookInfo{Title: d.Title}
		if len(d.Authors) > 0 {
			b.Author = d.Authors[0]
		}
		if d.Cover > 0 {
			b.CoverURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", d.Cover)
		}
		return b
	}
	return nil
}

// sameTitle guards against applying a different book's metadata: the
// result's title must contain the searched title's words (or vice versa).
func sameTitle(a, b string) bool {
	na, nb := normalize(a), normalize(b)
	return na != "" && nb != "" && (strings.Contains(nb, na) || strings.Contains(na, nb))
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func getJSON(ctx context.Context, u string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(v)
}

func fetchCover(ctx context.Context, u string) []byte {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	// Must be a real JPEG of reasonable size (placeholders are tiny).
	if err != nil || resp.StatusCode != http.StatusOK || len(b) < 2048 || !bytes.HasPrefix(b, []byte{0xFF, 0xD8}) {
		return nil
	}
	return b
}

// --- KEPUB -------------------------------------------------------------------

func convertKepub(ctx context.Context, file string) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.CreateTemp(filepath.Dir(file), ".ezkobo-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if err := kepub.NewConverter().Convert(ctx, out, &zr.Reader); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	zr.Close()
	return os.Rename(out.Name(), file)
}
