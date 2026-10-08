package main

import (
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/phoenixatom/ezkobo/internal/kobo"
)

// readingBook is a library book as the API shows it.
type readingBook struct {
	kobo.Book
	Cover bool   `json:"cover"`          // a cover image is available at /api/cover
	File  string `json:"file,omitempty"` // path under the library root, for sideloaded books
}

func (s *server) present(b kobo.Book) readingBook {
	rb := readingBook{Book: b}
	if b.ImageID != "" {
		if _, err := os.Stat(kobo.CoverPath(s.libraryRoot(), b.ImageID)); err == nil {
			rb.Cover = true
		}
	}
	if rel, ok := strings.CutPrefix(b.ID, "file://"+strings.TrimSuffix(s.libraryRoot(), "/")+"/"); ok {
		rb.File = rel
	}
	// The Kobo keeps a stale "rest of book" estimate after a book is finished.
	if b.Status == 2 || b.Progress >= 100 {
		rb.SecondsLeft = 0
	}
	rb.Description = "" // only in /api/book
	return rb
}

// reading reports what's being read now (minus books hidden in the app),
// recently finished books, and totals for the whole library.
func (s *server) reading(w http.ResponseWriter, r *http.Request) {
	books, err := kobo.Books(s.cfg.db, "")
	if err != nil {
		httpError(w, errors.New("the Kobo's library isn't available right now"), http.StatusServiceUnavailable)
		return
	}
	type stats struct {
		Books       int `json:"books"`
		Reading     int `json:"reading"`
		Finished    int `json:"finished"`
		NotStarted  int `json:"notStarted"`
		SecondsRead int `json:"secondsRead"`
	}
	hidden := map[string]bool{}
	for _, id := range s.loadSettings().Hidden {
		hidden[id] = true
	}
	var st stats
	var now, finished, timed, hiddenBooks []readingBook
	for _, b := range books {
		if hidden[b.ID] {
			hiddenBooks = append(hiddenBooks, s.present(b))
		}
		st.Books++
		st.SecondsRead += b.SecondsRead
		switch b.Status {
		case 1:
			st.Reading++
			if !hidden[b.ID] { // still counted, just not listed
				now = append(now, s.present(b))
			}
		case 2:
			st.Finished++
			finished = append(finished, s.present(b))
		default:
			st.NotStarted++
		}
		if b.SecondsRead > 0 {
			timed = append(timed, s.present(b))
		}
	}
	sort.Slice(now, func(i, j int) bool { return now[i].LastRead > now[j].LastRead })
	sort.Slice(finished, func(i, j int) bool {
		return max(finished[i].Finished, finished[i].LastRead) > max(finished[j].Finished, finished[j].LastRead)
	})
	sort.Slice(timed, func(i, j int) bool { return timed[i].SecondsRead > timed[j].SecondsRead })
	writeJSON(w, map[string]any{
		"reading":  orEmpty(now),
		"finished": orEmpty(finished[:min(len(finished), 20)]),
		"byTime":   orEmpty(timed[:min(len(timed), 20)]),
		"hidden":   orEmpty(hiddenBooks),
		"stats":    st,
	})
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// bookDetails returns one book, with its description, highlights and notes.
func (s *server) bookDetails(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	books, err := kobo.Books(s.cfg.db, id)
	if err != nil || id == "" || len(books) == 0 {
		httpError(w, errors.New("no such book in the Kobo's library"), http.StatusNotFound)
		return
	}
	rb := s.present(books[0])
	rb.Description = books[0].Description
	hs, _ := kobo.Highlights(s.cfg.db, id)
	writeJSON(w, map[string]any{"book": rb, "highlights": orEmpty(hs)})
}

// cover serves a book's cover image from the Kobo's own image cache.
func (s *server) cover(w http.ResponseWriter, r *http.Request) {
	books, err := kobo.Books(s.cfg.db, r.URL.Query().Get("id"))
	if err != nil || len(books) == 0 || books[0].ImageID == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, kobo.CoverPath(s.libraryRoot(), books[0].ImageID))
}
