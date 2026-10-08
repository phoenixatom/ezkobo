package kobo

import (
	"database/sql"
	"net/url"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// LibraryEntry is what the Kobo's own library knows about a book file.
type LibraryEntry struct {
	Title    string
	Author   string
	Progress int // percent read
}

// Library reads Nickel's database at dbPath (read-only) and returns its
// sideloaded books keyed by path relative to root, e.g. "Books/Dune.epub".
// It returns nil when the database isn't available (off-device, USB mode).
func Library(dbPath, root string) map[string]LibraryEntry {
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil
	}
	defer db.Close()

	// ContentType 6 = a book (chapters and annotations use other types).
	rows, err := db.Query(`SELECT ContentID, COALESCE(Title, ''), COALESCE(Attribution, ''),
		COALESCE(___PercentRead, 0) FROM content WHERE ContentType = 6 AND ContentID LIKE 'file://%'`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	prefix := "file://" + strings.TrimSuffix(root, "/") + "/"
	lib := map[string]LibraryEntry{}
	for rows.Next() {
		var id string
		var e LibraryEntry
		if rows.Scan(&id, &e.Title, &e.Author, &e.Progress) != nil {
			continue
		}
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		rel := strings.TrimPrefix(id, prefix)
		if u, err := url.PathUnescape(rel); err == nil {
			rel = u
		}
		lib[rel] = e
	}
	return lib
}
