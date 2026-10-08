package kobo

import (
	"database/sql"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strings"
)

// Book is a book in the Kobo's library, sideloaded or from the Kobo store,
// with what the Kobo knows about reading it.
type Book struct {
	ID           string `json:"id"` // Nickel's ContentID
	Title        string `json:"title"`
	Author       string `json:"author,omitempty"`
	Series       string `json:"series,omitempty"`
	SeriesNumber string `json:"seriesNumber,omitempty"`
	Status       int    `json:"status"`   // 0 not started, 1 reading, 2 finished
	Progress     int    `json:"progress"` // percent read
	SecondsRead  int    `json:"secondsRead,omitempty"`
	SecondsLeft  int    `json:"secondsLeft,omitempty"` // Kobo's estimate for the rest of the book
	LastRead     string `json:"lastRead,omitempty"`    // ISO 8601
	Finished     string `json:"finished,omitempty"`
	TimesStarted int    `json:"timesStarted,omitempty"`
	Pages        int    `json:"pages,omitempty"`
	Words        int    `json:"words,omitempty"`
	Publisher    string `json:"publisher,omitempty"`
	ISBN         string `json:"isbn,omitempty"`
	Description  string `json:"description,omitempty"`
	ImageID      string `json:"-"`
}

// Highlight is a highlight or note made while reading.
type Highlight struct {
	Text    string `json:"text,omitempty"`
	Note    string `json:"note,omitempty"`
	Created string `json:"created,omitempty"`
	Type    string `json:"type"` // "highlight" or "note"
}

func openDB(dbPath string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(2000)")
}

// Columns differ a little between firmware versions; each is optional.
var bookColumns = []struct {
	expr string
	dst  func(*Book) any
}{
	{"ContentID", func(b *Book) any { return &b.ID }},
	{"Title", func(b *Book) any { return &b.Title }},
	{"Attribution", func(b *Book) any { return &b.Author }},
	{"Series", func(b *Book) any { return &b.Series }},
	{"SeriesNumber", func(b *Book) any { return &b.SeriesNumber }},
	{"ReadStatus", func(b *Book) any { return &b.Status }},
	{"___PercentRead", func(b *Book) any { return &b.Progress }},
	{"TimeSpentReading", func(b *Book) any { return &b.SecondsRead }},
	{"RestOfBookEstimate", func(b *Book) any { return &b.SecondsLeft }},
	{"DateLastRead", func(b *Book) any { return &b.LastRead }},
	{"LastTimeFinishedReading", func(b *Book) any { return &b.Finished }},
	{"TimesStartedReading", func(b *Book) any { return &b.TimesStarted }},
	{"___NumPages", func(b *Book) any { return &b.Pages }},
	{"WordCount", func(b *Book) any { return &b.Words }},
	{"Publisher", func(b *Book) any { return &b.Publisher }},
	{"ISBN", func(b *Book) any { return &b.ISBN }},
	{"Description", func(b *Book) any { return &b.Description }},
	{"ImageId", func(b *Book) any { return &b.ImageID }},
}

// Books returns the books in the Kobo's library. With id set, only that book.
func Books(dbPath, id string) ([]Book, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	present := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(content)`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil {
			present[name] = true
		}
	}
	rows.Close()

	var exprs []string
	var used []func(*Book) any
	for _, c := range bookColumns {
		if present[c.expr] {
			// COALESCE keeps NULLs out of Go's string and int fields.
			zero := "''"
			if c.expr == "ReadStatus" || strings.HasPrefix(c.expr, "___") || strings.HasPrefix(c.expr, "Time") ||
				c.expr == "RestOfBookEstimate" || c.expr == "WordCount" {
				zero = "0"
			}
			exprs = append(exprs, fmt.Sprintf("COALESCE(%s, %s)", c.expr, zero))
			used = append(used, c.dst)
		}
	}
	if !present["ContentID"] || !present["ContentType"] {
		return nil, fmt.Errorf("unexpected library database layout")
	}
	query := "SELECT " + strings.Join(exprs, ", ") + " FROM content WHERE ContentType = 6"
	var args []any
	if id != "" {
		query += " AND ContentID = ?"
		args = append(args, id)
	}
	rows, err = db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var books []Book
	for rows.Next() {
		var b Book
		dsts := make([]any, len(used))
		for i, f := range used {
			dsts[i] = f(&b)
		}
		if rows.Scan(dsts...) != nil {
			continue
		}
		b.Description = plainText(b.Description)
		b.Pages, b.Words = max(b.Pages, 0), max(b.Words, 0) // -1 means unknown
		books = append(books, b)
	}
	return books, rows.Err()
}

var tags = regexp.MustCompile(`<[^>]*>`)

// plainText turns a store description (HTML) into plain paragraphs.
func plainText(s string) string {
	s = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n\n").Replace(s)
	s = html.UnescapeString(tags.ReplaceAllString(s, ""))
	var paras []string
	for _, p := range strings.Split(s, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, p)
		}
	}
	return strings.Join(paras, "\n\n")
}

// Highlights returns the visible highlights and notes in a book, in order.
func Highlights(dbPath, bookID string) ([]Highlight, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT COALESCE(Text, ''), COALESCE(Annotation, ''), COALESCE(DateCreated, ''), COALESCE(Type, '')
		FROM Bookmark WHERE VolumeID = ? AND COALESCE(Hidden, 'false') <> 'true' AND Type IN ('highlight', 'note')
		ORDER BY COALESCE(ChapterProgress, 0), DateCreated`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hs []Highlight
	for rows.Next() {
		var h Highlight
		if rows.Scan(&h.Text, &h.Note, &h.Created, &h.Type) == nil {
			h.Text = strings.TrimSpace(h.Text)
			h.Note = strings.TrimSpace(h.Note)
			if h.Text != "" || h.Note != "" {
				hs = append(hs, h)
			}
		}
	}
	return hs, rows.Err()
}

// CoverPath is where Nickel keeps a book's cover (a JPEG), under the user
// storage root. The two folder levels come from a hash of the image ID; this
// is the scheme Calibre's Kobo driver uses.
func CoverPath(onboard, imageID string) string {
	var h uint32
	for _, c := range []byte(imageID) {
		h = (h << 4) + uint32(c)
		h ^= (h & 0xf0000000) >> 23
		h &= 0x0fffffff
	}
	return filepath.Join(onboard, ".kobo-images", fmt.Sprint(h&0xff), fmt.Sprint((h&0xff00)>>8),
		imageID+" - N3_LIBRARY_FULL.parsed")
}
