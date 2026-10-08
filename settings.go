package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Settings are stored on the Kobo itself, so every client (app, Share sheet,
// browser) gets the same behaviour, and each Kobo can be set up differently.
type settings struct {
	// Convert EPUBs to Kobo's KEPUB format on arrival.
	Kepub bool `json:"kepub"`
	// Look up title, author and cover on Open Library when a book's own
	// metadata is missing or messy. Sends the title and author to Open Library.
	Metadata bool `json:"metadata"`
	// Save books as "Author - Title".
	CleanNames bool `json:"cleanNames"`
	// Where to look up details, in order. Each can be switched off.
	Providers []provider `json:"providers"`
	// Optional Google Books API key; keyless requests share a public daily
	// quota that is often used up. Never sent back to clients.
	GoogleAPIKey string `json:"googleApiKey,omitempty"`
	// Hardcover API token (required for Hardcover). Never sent back to clients.
	HardcoverToken string `json:"hardcoverToken,omitempty"`
}

type provider struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// Default order, best first for recent and translated books. Hardcover needs
// a token, so it's off until one is added.
var allProviders = []provider{
	{"apple", true}, {"openlibrary", true}, {"google", true}, {"hardcover", false},
}

var defaultSettings = settings{Kepub: true, Metadata: true, CleanNames: true}

// normalizeProviders keeps known providers in the given order, then appends
// any missing ones with their defaults.
func normalizeProviders(ps []provider) []provider {
	var out []provider
	seen := map[string]bool{}
	for _, p := range ps {
		for _, known := range allProviders {
			if p.ID == known.ID && !seen[p.ID] {
				out = append(out, p)
				seen[p.ID] = true
			}
		}
	}
	for _, known := range allProviders {
		if !seen[known.ID] {
			out = append(out, known)
		}
	}
	return out
}

func (s *server) settingsPath() string { return filepath.Join(s.cfg.stateDir, "settings.json") }
func (s *server) pinPath() string      { return filepath.Join(s.cfg.stateDir, "pin") }

func (s *server) loadSettings() settings {
	st := defaultSettings
	if b, err := os.ReadFile(s.settingsPath()); err == nil {
		json.Unmarshal(b, &st)
	}
	st.Providers = normalizeProviders(st.Providers)
	return st
}

func (s *server) saveSettings(st settings) error {
	b, _ := json.MarshalIndent(st, "", "  ")
	os.MkdirAll(s.cfg.stateDir, 0o755)
	return os.WriteFile(s.settingsPath(), append(b, '\n'), 0o644)
}

// readPIN returns the PIN, or "" when none is set (the default).
func readPIN(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(stateDir, "pin"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func validPIN(p string) bool {
	if len(p) < 4 || len(p) > 8 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// pinGuard rate-limits wrong PINs: after 5 wrong tries in a row, every
// attempt is refused for 30 seconds.
type pinGuard struct {
	mu       sync.Mutex
	failures int
	until    time.Time
}

func (g *pinGuard) check(want, got string) (ok bool, wait time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if d := time.Until(g.until); d > 0 {
		return false, d
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1 {
		g.failures = 0
		return true, 0
	}
	g.failures++
	if g.failures >= 5 {
		g.failures = 0
		g.until = time.Now().Add(30 * time.Second)
	}
	return false, 0
}

// requirePIN protects a handler when a PIN is set. Clients send it in the
// X-EzKobo-PIN header.
func (s *server) requirePIN(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pin := readPIN(s.cfg.stateDir)
		if pin == "" {
			h(w, r)
			return
		}
		ok, wait := s.pins.check(pin, r.Header.Get("X-EzKobo-PIN"))
		if wait > 0 {
			w.Header().Set("Retry-After", "30")
			httpError(w, errors.New("too many wrong PINs, try again in 30 seconds"), http.StatusTooManyRequests)
			return
		}
		if !ok {
			httpError(w, errors.New("PIN required"), http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// publicSettings is what clients see: whether a Google key is set, not the key.
func publicSettings(st settings) map[string]any {
	return map[string]any{
		"kepub":             st.Kepub,
		"metadata":          st.Metadata,
		"cleanNames":        st.CleanNames,
		"providers":         st.Providers,
		"googleApiKeySet":   st.GoogleAPIKey != "",
		"hardcoverTokenSet": st.HardcoverToken != "",
	}
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"settings": publicSettings(s.loadSettings()), "pin": readPIN(s.cfg.stateDir) != ""})
}

// putSettings changes only the fields present in the request. An empty
// googleApiKey removes the key.
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kepub          *bool       `json:"kepub"`
		Metadata       *bool       `json:"metadata"`
		CleanNames     *bool       `json:"cleanNames"`
		Providers      *[]provider `json:"providers"`
		GoogleAPIKey   *string     `json:"googleApiKey"`
		HardcoverToken *string     `json:"hardcoverToken"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	st := s.loadSettings()
	if req.Kepub != nil {
		st.Kepub = *req.Kepub
	}
	if req.Metadata != nil {
		st.Metadata = *req.Metadata
	}
	if req.CleanNames != nil {
		st.CleanNames = *req.CleanNames
	}
	if req.Providers != nil {
		st.Providers = normalizeProviders(*req.Providers)
	}
	if req.GoogleAPIKey != nil {
		key := strings.TrimSpace(*req.GoogleAPIKey)
		if len(key) > 200 || strings.ContainsAny(key, " \t\n/?&#") {
			httpError(w, errors.New("that doesn't look like a Google API key"), http.StatusBadRequest)
			return
		}
		st.GoogleAPIKey = key
	}
	if req.HardcoverToken != nil {
		// Accept the token with or without the "Bearer " Hardcover shows.
		token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(*req.HardcoverToken), "Bearer "))
		if len(token) > 2000 || strings.ContainsAny(token, " \t\n") {
			httpError(w, errors.New("that doesn't look like a Hardcover token"), http.StatusBadRequest)
			return
		}
		st.HardcoverToken = token
	}
	if err := s.saveSettings(st); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"settings": publicSettings(st)})
}

// putPIN sets or changes the PIN (the current one is required if set).
func (s *server) putPIN(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil || !validPIN(body.PIN) {
		httpError(w, errors.New("the PIN must be 4 to 8 digits"), http.StatusBadRequest)
		return
	}
	os.MkdirAll(s.cfg.stateDir, 0o755)
	if err := os.WriteFile(s.pinPath(), []byte(body.PIN+"\n"), 0o600); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"pin": true})
}

func (s *server) deletePIN(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(s.pinPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"pin": false})
}
