package kobo

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestCoverPath(t *testing.T) {
	// A real cover location from a Clara Colour's .kobo-images folder.
	got := CoverPath("/mnt/onboard", "707b8766-e875-4b19-88b6-b2bbf4bfb75e")
	want := "/mnt/onboard/.kobo-images/149/203/707b8766-e875-4b19-88b6-b2bbf4bfb75e - N3_LIBRARY_FULL.parsed"
	if got != want {
		t.Fatalf("CoverPath = %q, want %q", got, want)
	}
}

func TestBooksAndHighlights(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "KoboReader.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// A subset of Nickel's columns: Books must cope with missing ones.
	for _, q := range []string{
		`CREATE TABLE content (ContentID TEXT, ContentType INTEGER, Title TEXT, Attribution TEXT,
			ReadStatus INTEGER, ___PercentRead INTEGER, TimeSpentReading INTEGER, RestOfBookEstimate INTEGER,
			DateLastRead TEXT, Series TEXT, SeriesNumber TEXT, Description TEXT, ImageId TEXT)`,
		`INSERT INTO content VALUES ('file:///mnt/onboard/Books/Emma.kepub.epub', 6, 'Emma', 'Jane Austen',
			1, 42, 3600, 5400, '2026-10-08T19:51:21Z', NULL, NULL, '<p>A novel &amp; a comedy.</p><p>Second.</p>', 'img1')`,
		`INSERT INTO content VALUES ('store-uuid', 6, 'Persuasion', 'Jane Austen', 2, 100, 7200, 0, '', 'Austen', '6', NULL, 'img2')`,
		`INSERT INTO content VALUES ('store-uuid!!chapter1', 9, 'Chapter 1', '', 0, 0, 0, 0, '', NULL, NULL, NULL, '')`,
		`ALTER TABLE content ADD COLUMN BookID TEXT`,
		`ALTER TABLE content ADD COLUMN VolumeIndex INTEGER`,
		`ALTER TABLE content ADD COLUMN ___FileSize REAL`,
		// Two chapters: 25% and 75% of the book.
		`INSERT INTO content (ContentID, ContentType, Title, BookID, VolumeIndex, ___FileSize) VALUES
			('store-uuid!ch1', 9, 'Chapter 1', 'store-uuid', 0, 25),
			('store-uuid!ch2', 9, 'Chapter 2', 'store-uuid', 1, 75)`,
		`CREATE TABLE Bookmark (VolumeID TEXT, ContentID TEXT, Text TEXT, Annotation TEXT, DateCreated TEXT, Type TEXT, Hidden TEXT, ChapterProgress REAL)`,
		// Created out of reading order, to check sorting by position.
		`INSERT INTO Bookmark VALUES ('store-uuid', 'store-uuid!ch2#anchor-7', ' Second quote ', '', '2026-10-01', 'highlight', 'false', 0.5),
			('store-uuid', 'store-uuid!ch1', 'First quote', 'My note', '2026-10-02', 'note', 'false', 0.2),
			('store-uuid', 'store-uuid!ch1', 'Deleted', '', '2026-10-03', 'highlight', 'true', 0.9),
			('store-uuid', 'store-uuid!ch1', '', '', '2026-10-03', 'dogear', 'false', 0.2)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	books, err := Books(dbPath, "")
	if err != nil || len(books) != 2 {
		t.Fatalf("Books = %+v, %v", books, err)
	}
	e := books[0]
	if e.Title != "Emma" || e.Status != 1 || e.Progress != 42 || e.SecondsLeft != 5400 || e.ImageID != "img1" {
		t.Fatalf("Emma = %+v", e)
	}
	if e.Description != "A novel & a comedy.\n\nSecond." {
		t.Fatalf("description = %q", e.Description)
	}
	one, _ := Books(dbPath, "store-uuid")
	if len(one) != 1 || one[0].Series != "Austen" || one[0].SeriesNumber != "6" {
		t.Fatalf("Persuasion = %+v", one)
	}

	hs, err := Highlights(dbPath, "store-uuid")
	if err != nil || len(hs) != 2 || hs[0].Text != "First quote" || hs[0].Note != "My note" || hs[1].Text != "Second quote" {
		t.Fatalf("Highlights = %+v, %v", hs, err)
	}
	// 0.2 into a 25% chapter = 5%; half-way through the 75% chapter after it = 62.5%.
	if hs[0].Position == nil || *hs[0].Position != 5 || hs[1].Position == nil || *hs[1].Position != 62.5 {
		t.Fatalf("positions = %v, %v", hs[0].Position, hs[1].Position)
	}
}
