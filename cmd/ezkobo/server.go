package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/phoenixatom/ezkobo/internal/book"
	"github.com/phoenixatom/ezkobo/internal/kobo"
	"github.com/phoenixatom/ezkobo/internal/mdns"
)

//go:embed web/index.html
var indexHTML []byte

type server struct {
	cfg      config
	mu       sync.Mutex
	lastSeen time.Time
	active   int
	rescanMu sync.Mutex
	pins     pinGuard
}

func serve(cfg config) error {
	// Both logs are size-capped; stderr goes nowhere when started by "start".
	writers := []io.Writer{os.Stderr, &appendLog{path: cfg.logfile}}
	if cfg.onboardLog != "" {
		writers = append(writers, &appendLog{path: cfg.onboardLog})
	}
	log.SetOutput(io.MultiWriter(writers...))

	// Take the port first: if another copy is already running, quit before
	// announcing anything (a second copy's mDNS goodbye would make phones
	// forget the Kobo) and without touching the running copy's pid file.
	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		log.Printf("ezkobo: not starting, %s is in use (already running?): %v", cfg.addr, err)
		return nil
	}
	os.WriteFile(cfg.pidfile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(cfg.pidfile)

	s := &server{cfg: cfg, lastSeen: time.Now()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("GET /api/info", s.track(s.info))
	// Everything below needs the PIN, if one is set.
	mux.HandleFunc("GET /api/books", s.track(s.requirePIN(s.listBooks)))
	mux.HandleFunc("PUT /api/books/{name}", s.track(s.requirePIN(s.putBook)))
	mux.HandleFunc("DELETE /api/books/{path...}", s.track(s.requirePIN(s.deleteBook)))
	mux.HandleFunc("POST /api/rescan", s.track(s.requirePIN(s.rescan)))
	mux.HandleFunc("GET /api/reading", s.track(s.requirePIN(s.reading)))
	mux.HandleFunc("GET /api/book", s.track(s.requirePIN(s.bookDetails)))
	mux.HandleFunc("GET /api/cover", s.track(s.requirePIN(s.cover)))
	mux.HandleFunc("GET /api/settings", s.track(s.requirePIN(s.getSettings)))
	mux.HandleFunc("PUT /api/settings", s.track(s.requirePIN(s.putSettings)))
	mux.HandleFunc("PUT /api/pin", s.track(s.requirePIN(s.putPIN)))
	mux.HandleFunc("DELETE /api/pin", s.track(s.requirePIN(s.deletePIN)))

	srv := &http.Server{Handler: logRequests(mux), ReadHeaderTimeout: 30 * time.Second}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	svc := &mdns.Service{
		Host:     strings.ToLower(cfg.host) + ".local",
		Instance: strings.ReplaceAll(cfg.name, ".", "-"),
		Port:     listenPort(cfg.addr),
		Model:    cfg.model,
	}
	mdnsDone := make(chan struct{})
	go func() { mdns.Run(ctx, svc); close(mdnsDone) }()
	if cfg.idle > 0 {
		go s.idleWatch(ctx, cancel)
	}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		srv.Shutdown(sctx)
	}()

	log.Printf("ezkobo: started (pid %d) as %q, serving %s on %s, addresses %v",
		os.Getpid(), cfg.name, cfg.dir, cfg.addr, mdns.LocalIPv4s())
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	select { // let mDNS send its goodbye
	case <-mdnsDone:
	case <-time.After(4 * time.Second):
	}
	log.Printf("ezkobo: stopped")
	return nil
}

// logRequests logs each API request (not health checks) for debugging.
func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ping" {
			log.Printf("ezkobo: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		}
		h.ServeHTTP(w, r)
	})
}

// appendLog appends each write to a file, opening and closing it every time
// so no file stays open on /mnt/onboard (which would block USB mass storage).
// Writes fail silently while the storage is unmounted.
type appendLog struct {
	path string
	mu   sync.Mutex
}

func (l *appendLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if fi, err := os.Stat(l.path); err == nil && fi.Size() > 256<<10 {
		os.Rename(l.path, l.path+".old")
	}
	os.MkdirAll(filepath.Dir(l.path), 0o755)
	if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		f.Write(p)
		f.Close()
	}
	return len(p), nil
}

// track records activity so the idle watcher doesn't stop mid-transfer.
func (s *server) track(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.active++
		s.lastSeen = time.Now()
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.active--
			s.lastSeen = time.Now()
			s.mu.Unlock()
		}()
		h(w, r)
	}
}

func (s *server) idleWatch(ctx context.Context, stop context.CancelFunc) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			idle := s.active == 0 && time.Since(s.lastSeen) > s.cfg.idle
			s.mu.Unlock()
			if idle {
				log.Printf("ezkobo: idle for %s, shutting down", s.cfg.idle)
				stop()
				return
			}
		}
	}
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

// bookFile is a book on the Kobo, as listed by the API.
type bookFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // unix seconds
	// From the Kobo's library database; InLibrary is null when unknown.
	Title     string `json:"title,omitempty"`
	Author    string `json:"author,omitempty"`
	InLibrary *bool  `json:"inLibrary"`
	// Nickel's ContentID, for /api/book and /api/cover.
	ID       string `json:"id"`
	Progress int    `json:"progress,omitempty"`
}

func (s *server) listBooks(w http.ResponseWriter, r *http.Request) {
	books, err := s.allBooks()
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	for i := range books {
		books[i].ID = "file://" + strings.TrimSuffix(s.libraryRoot(), "/") + "/" + books[i].Name
	}
	if lib := kobo.Library(s.cfg.db, s.libraryRoot()); lib != nil {
		for i := range books {
			e, ok := lib[books[i].Name]
			books[i].InLibrary = &ok
			books[i].Title, books[i].Author, books[i].Progress = e.Title, e.Author, e.Progress
		}
	}
	sort.Slice(books, func(i, j int) bool { return books[i].MTime > books[j].MTime })
	free, _ := s.space()
	writeJSON(w, map[string]any{"books": books, "free": free, "dir": s.cfg.dir})
}

// libraryRoot is where books are listed from: the whole user storage, so
// books not sent by EzKobo show up too. Falls back to the upload folder
// when running off-device.
func (s *server) libraryRoot() string {
	if _, err := os.Stat(s.cfg.library); err == nil {
		return s.cfg.library
	}
	return s.cfg.dir
}

// allBooks returns every book under the library root, named by its path
// relative to the root ("Books/Dune.epub"). Hidden folders (.kobo, .adds…)
// and KOReader's .sdr sidecar folders are skipped.
func (s *server) allBooks() ([]bookFile, error) {
	root := s.libraryRoot()
	books := []bookFile{}
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			if p == root && errors.Is(err, os.ErrNotExist) {
				return filepath.SkipAll
			}
			return nil // unreadable entry: skip it, keep going
		}
		name := e.Name()
		if p != root && strings.HasPrefix(name, ".") {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			if strings.HasSuffix(strings.ToLower(name), ".sdr") {
				return filepath.SkipDir
			}
			return nil
		}
		if !book.IsBook(name) {
			return nil
		}
		if fi, err := e.Info(); err == nil {
			rel, _ := filepath.Rel(root, p)
			books = append(books, bookFile{Name: filepath.ToSlash(rel), Size: fi.Size(), MTime: fi.ModTime().Unix()})
		}
		return nil
	})
	return books, err
}

func (s *server) info(w http.ResponseWriter, r *http.Request) {
	d := kobo.ReadDevice()
	free, total := s.space()
	books, _ := s.allBooks()
	bat := kobo.ReadBattery()
	if s.cfg.demo {
		if n := len(s.cfg.name); n >= 4 {
			d.Serial = s.cfg.name[n-4:]
		}
		level := 0
		for _, c := range s.cfg.name {
			level += int(c)
		}
		bat = &kobo.Battery{Level: 55 + level%40}
	}
	writeJSON(w, map[string]any{
		"name":     s.cfg.name,
		"model":    s.cfg.model,
		"serial":   d.Serial,
		"firmware": d.Firmware,
		"battery":  bat,
		"free":     free,
		"total":    total,
		"books":    len(books),
		"locked":   readPIN(s.cfg.stateDir) != "",
		// What to call this Kobo: the name set in the app, else the model.
		"displayName": s.displayName(),
	})
}

// space reports free and total bytes of the storage holding the books folder.
func (s *server) space() (free, total uint64) {
	if s.cfg.demo {
		return 24_300_000_000, 29_800_000_000
	}
	for dir := s.cfg.dir; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(dir); err == nil || dir == filepath.Dir(dir) {
			return kobo.DiskSpace(dir)
		}
	}
}

func (s *server) putBook(w http.ResponseWriter, r *http.Request) {
	name, err := book.CleanName(r.PathValue("name"))
	if err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}

	if fi, err := os.Stat(filepath.Join(s.cfg.dir, name)); err == nil && r.ContentLength >= 0 && fi.Size() == r.ContentLength {
		writeJSON(w, map[string]any{"name": name, "skipped": true})
		return
	}

	os.MkdirAll(s.cfg.dir, 0o755)
	tmp, err := os.CreateTemp(s.cfg.dir, ".ezkobo-*.part")
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())

	// FAT32 can't hold files of 4 GiB or more.
	n, err := io.Copy(tmp, http.MaxBytesReader(w, r.Body, 4<<30-1))
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	// Fix metadata, convert to KEPUB and rename, per this Kobo's settings.
	res := book.Process(r.Context(), s.loadSettings(), tmp.Name(), name)
	dst := filepath.Join(s.cfg.dir, res.Name)
	if _, err := os.Stat(dst); err == nil {
		writeJSON(w, map[string]any{"name": res.Name, "skipped": true})
		return
	}
	os.Chmod(tmp.Name(), 0o644)
	if err := os.Rename(tmp.Name(), dst); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	log.Printf("ezkobo: received %s as %s (%d bytes, kepub=%v, metadata=%q)", name, res.Name, n, res.Converted, res.Metadata)
	toast("Received " + res.Name)
	writeJSON(w, map[string]any{"name": res.Name, "size": n, "converted": res.Converted, "metadata": res.Metadata})
}

func (s *server) deleteBook(w http.ResponseWriter, r *http.Request) {
	rel, err := cleanRelPath(r.PathValue("path"))
	if err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	p := filepath.Join(s.libraryRoot(), filepath.FromSlash(rel))
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		httpError(w, errors.New("no such book"), http.StatusNotFound)
		return
	}
	if err := os.Remove(p); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	log.Printf("ezkobo: deleted %s", rel)
	writeJSON(w, map[string]any{"deleted": rel})
}

// cleanRelPath validates a book path relative to the library root: no
// escaping the root, no hidden folders, and it must be a book.
func cleanRelPath(p string) (string, error) {
	p = path.Clean("/" + strings.ReplaceAll(p, `\`, "/"))[1:]
	if p == "" {
		return "", errors.New("invalid path")
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") {
			return "", errors.New("invalid path")
		}
	}
	if !book.IsBook(p) {
		return "", fmt.Errorf("%s: not a book", p)
	}
	return p, nil
}

func (s *server) rescan(w http.ResponseWriter, r *http.Request) {
	s.rescanMu.Lock()
	defer s.rescanMu.Unlock()
	method, err := rescanLibrary(s.cfg.rescan)
	if err != nil {
		log.Printf("ezkobo: rescan: %v", err)
	}
	writeJSON(w, map[string]any{"method": method, "ok": err == nil})
}

func listenPort(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// displayName is the name set in the app, or the model.
func (s *server) displayName() string {
	if n := s.loadSettings().Name; n != "" {
		return n
	}
	return s.cfg.model
}
