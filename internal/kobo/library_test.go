package kobo

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

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

	lib := Library(dbPath, root)
	e, ok := lib["Books/Emma.kepub.epub"]
	if !ok || e.Title != "Emma" || e.Author != "Jane Austen" || e.Progress != 42 || len(lib) != 1 {
		t.Fatalf("library = %+v", lib)
	}
}
