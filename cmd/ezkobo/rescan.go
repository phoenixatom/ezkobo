package main

import (
	"errors"
	"os"
	"os/exec"
)

// NickelDBus (https://github.com/shermp/NickelDBus) installs qndb, which can
// ask Nickel (Kobo's UI) to import books and show toasts.
func qndb() string {
	for _, p := range []string{"/usr/bin/qndb", "/usr/local/bin/qndb"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// rescanLibrary makes Nickel import newly added books. It returns the
// method used; "none" means the user has to start the import on the Kobo
// (NickelMenu's "Import new books", or a restart).
//
// There is deliberately no fallback that fakes USB plug events through
// /tmp/nickel-hardware-status: on current firmware it doesn't import
// anything and can leave Nickel ignoring a real USB connection.
func rescanLibrary(mode string) (string, error) {
	if mode == "off" {
		return "off", nil
	}
	if q := qndb(); q != "" {
		// Not pfmRescanBooksFull: that imitates a USB connect/disconnect,
		// which also starts a Kobo sync.
		return "qndb", exec.Command(q, "-m", "pfmRescanBooks").Run()
	}
	return "none", errors.New("NickelDBus not installed")
}

// toast shows a short message on the Kobo screen, if NickelDBus is installed.
func toast(msg string) {
	if q := qndb(); q != "" {
		exec.Command(q, "-m", "mwcToast", "2000", msg).Start()
	}
}
