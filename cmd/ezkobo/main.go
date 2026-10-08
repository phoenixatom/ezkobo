// ezkobo is a tiny agent that runs on a Kobo e-reader and lets you send
// books to it from your phone over Wi-Fi: the EzKobo iPhone app finds it via
// Bonjour (or use a browser). No internet, no cloud, no USB cable.
//
// Usage:
//
//	ezkobo start   # start the server in the background and print the URL
//	ezkobo status  # print the URL if running
//	ezkobo stop    # stop the background server
//	ezkobo serve   # run the server in the foreground
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/phoenixatom/ezkobo/internal/kobo"
	"github.com/phoenixatom/ezkobo/internal/mdns"
)

type config struct {
	addr    string
	dir     string
	library string
	host    string
	name    string
	model   string
	idle    time.Duration
	pidfile string
	logfile string
	// A copy of the log on the user storage, readable over USB.
	onboardLog string
	rescan     string
	demo       bool // made-up device details, for screenshots
	stateDir   string
	db         string
}

func main() {
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	var cfg config
	fs := flag.NewFlagSet("ezkobo", flag.ExitOnError)
	fs.StringVar(&cfg.addr, "addr", ":80", "listen address")
	fs.StringVar(&cfg.dir, "dir", "/mnt/onboard/Books", "folder where received books are saved")
	fs.StringVar(&cfg.library, "library", "/mnt/onboard", "folder whose books are listed (falls back to -dir if missing)")
	fs.StringVar(&cfg.host, "host", kobo.DefaultHost(), "mDNS name (reachable as http://<host>.local)")
	fs.StringVar(&cfg.name, "name", kobo.Name(), "name shown in the phone app")
	fs.StringVar(&cfg.model, "model", kobo.ReadDevice().Model, "Kobo model shown in the phone app")
	fs.DurationVar(&cfg.idle, "idle", 0, "stop after this long without requests (0 = never)")
	fs.StringVar(&cfg.pidfile, "pidfile", "/tmp/ezkobo.pid", "pid file")
	// Not on /mnt/onboard: an open file there would block USB mass storage.
	fs.StringVar(&cfg.logfile, "log", "/tmp/ezkobo.log", "log file")
	fs.StringVar(&cfg.onboardLog, "onboard-log", "/mnt/onboard/.adds/ezkobo/ezkobo.log", "log copy readable over USB (\"\" to disable)")
	fs.StringVar(&cfg.stateDir, "state", "/mnt/onboard/.adds/ezkobo", "folder for settings and the PIN")
	fs.StringVar(&cfg.db, "db", "/mnt/onboard/.kobo/KoboReader.sqlite", "Kobo library database (read-only)")
	fs.BoolVar(&cfg.demo, "demo", false, "development: report made-up battery, serial and 32 GB storage (for screenshots)")
	fs.StringVar(&cfg.rescan, "rescan", "auto", "how to make Kobo import new books: auto (NickelDBus if installed) or off")
	fs.Parse(args)

	var err error
	switch cmd {
	case "serve":
		err = serve(cfg)
	case "start":
		err = start(cfg, args)
	case "stop":
		err = stop(cfg)
	case "status":
		err = status(cfg, 0)
	case "pin":
		err = pinCommand(cfg, fs.Args())
	default:
		err = fmt.Errorf("unknown command %q (use start, stop, status or serve)", cmd)
	}
	if err != nil {
		fmt.Println("EzKobo:", err)
		os.Exit(1)
	}
}

// start launches "ezkobo serve" detached from the caller (NickelMenu), then
// waits for WiFi and prints where to connect.
func start(cfg config, args []string) error {
	if !running(cfg) {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		// serve writes its own (size-capped) logs; no stdout/stderr needed.
		c := exec.Command(exe, append([]string{"serve"}, args...)...)
		c.Dir = "/"
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := c.Start(); err != nil {
			return err
		}
		c.Process.Release()
		for deadline := time.Now().Add(1500 * time.Millisecond); !running(cfg) && time.Now().Before(deadline); {
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Answer right away: NickelMenu's cmd_output gives up after 10s.
	return status(cfg, 0)
}

func stop(cfg config) error {
	b, err := os.ReadFile(cfg.pidfile)
	if err != nil {
		fmt.Println("EzKobo is not running.")
		return nil
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if p, err := os.FindProcess(pid); err == nil && pid > 0 {
		p.Signal(syscall.SIGTERM)
	}
	os.Remove(cfg.pidfile)
	fmt.Println("EzKobo stopped.")
	return nil
}

// status prints the URLs to open on the phone. It waits up to wait for the
// Kobo to get an IP address, since WiFi may still be connecting.
func status(cfg config, wait time.Duration) error {
	if !running(cfg) {
		msg := "not running."
		if b, err := os.ReadFile(cfg.logfile); err == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			msg += "\n\nLast log lines:\n" + strings.Join(lines[max(0, len(lines)-5):], "\n")
		}
		return errors.New(msg)
	}
	deadline := time.Now().Add(wait)
	ips := mdns.LocalIPv4s()
	for len(ips) == 0 && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		ips = mdns.LocalIPv4s()
	}

	port := ""
	if _, p, err := net.SplitHostPort(cfg.addr); err == nil && p != "80" {
		port = ":" + p
	}
	fmt.Printf("EzKobo is running as \"%s\".\n", cfg.name)
	if pin := readPIN(cfg.stateDir); pin != "" {
		fmt.Printf("PIN: %s\n", pin)
	}
	fmt.Println()
	if len(ips) == 0 {
		fmt.Println("WiFi is off. Turn it on and the EzKobo")
		fmt.Println("app on your phone will find this Kobo.")
		return nil
	}
	fmt.Println("Open the EzKobo app on your phone, or a browser at:")
	for _, ip := range ips {
		fmt.Printf("  http://%s%s\n", ip, port)
	}
	fmt.Printf("  http://%s.local%s\n", cfg.host, port)
	return nil
}

// running reports whether the agent is up, from its pid file. The agent
// writes the file only after it has the port, so a live pid means it's
// serving. (No HTTP self-check: loopback isn't reliable on every Kobo.)
func running(cfg config) bool {
	b, err := os.ReadFile(cfg.pidfile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	// Guard against a stale pid file whose pid now belongs to something else.
	if cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		return strings.Contains(string(cmd), "ezkobo")
	}
	return true // no /proc (macOS): trust the signal check
}

// pinCommand shows the PIN, or with "off" removes it (from the Kobo's menu,
// for when the PIN is forgotten).
func pinCommand(cfg config, args []string) error {
	path := filepath.Join(cfg.stateDir, "pin")
	if len(args) > 0 && args[0] == "off" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Println("EzKobo PIN removed. Phones can connect without one.")
		return nil
	}
	if pin := readPIN(cfg.stateDir); pin != "" {
		fmt.Println("EzKobo PIN:", pin)
	} else {
		fmt.Println("No EzKobo PIN is set. You can set one in the app.")
	}
	return nil
}
