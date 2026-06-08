// Package server implements the WebDAV HTTP handler for Warpbox.
//
// This file contains the branded HTML landing page served at the root
// URL (/). The Warpbox logo is compiled into the binary via Go's embed
// package so there are no external file dependencies at runtime.

package server

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"time"
)

//go:embed landing.html warpbox-sm.png
var landingFS embed.FS

// handleLanding serves the Warpbox branded landing page with the embedded
// logo rendered via an <img> tag referencing /warpbox.png.
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	html, err := landingFS.ReadFile("landing.html")
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, string(html))
}

// handleLogo serves the embedded warpbox.png at /warpbox.png and also at
// /favicon.ico, giving the landing page a branded browser tab icon.
func (s *Server) handleLogo(w http.ResponseWriter, r *http.Request) {
	png, err := landingFS.ReadFile("warpbox-sm.png")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "warpbox.png", time.Time{}, &byteReadSeeker{data: png})
}

// byteReadSeeker wraps a []byte so it can be passed to http.ServeContent.
type byteReadSeeker struct {
	data []byte
	off  int
}

func (b *byteReadSeeker) Read(p []byte) (int, error) {
	if b.off >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.off:])
	b.off += n
	return n, nil
}

func (b *byteReadSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		b.off = int(offset)
	case io.SeekCurrent:
		b.off += int(offset)
	case io.SeekEnd:
		b.off = len(b.data) + int(offset)
	}
	return int64(b.off), nil
}