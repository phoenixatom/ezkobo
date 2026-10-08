package book

import "testing"

func TestCleanName(t *testing.T) {
	ok := map[string]string{
		"Dune.epub":                 "Dune.epub",
		"../../etc/Dune.epub":       "Dune.epub",
		`C:\Books\Dune.EPUB`:        "Dune.EPUB",
		`What? A "Book".kepub.epub`: "What_ A _Book_.kepub.epub",
		"Comic.cbz":                 "Comic.cbz",
	}
	for in, want := range ok {
		got, err := CleanName(in)
		if err != nil || got != want {
			t.Errorf("CleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "..", ".hidden.epub", "virus.sh", "notes", "book.epub/.."} {
		if got, err := CleanName(in); err == nil {
			t.Errorf("CleanName(%q) = %q; want error", in, got)
		}
	}
}
