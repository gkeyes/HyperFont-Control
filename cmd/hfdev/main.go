// Local-only development server. This executable is never included in ZIPs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hyperfont.local/control/internal/engine"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	data := flag.String("data", "", "isolated test data directory")
	rom := flag.String("rom", "", "fixture ROM directory")
	web := flag.String("web", "module/webroot", "web assets")
	downloads := flag.String("downloads", "", "fixture Download folder (default: <data>-Download)")
	addr := flag.String("listen", "127.0.0.1:8876", "localhost only")
	flag.Parse()
	if *data == "" || *rom == "" || !strings.HasPrefix(*addr, "127.0.0.1:") {
		panic("explicit fixture/data and localhost address required")
	}
	root, _ := filepath.Abs(*data)
	fixture, _ := filepath.Abs(*rom)
	if fixture == "/" || strings.HasPrefix(root, "/data/adb") {
		panic("device paths prohibited")
	}
	s := &engine.Store{Root: root, RomRoot: fixture, Runtime: filepath.Join(root, "fake-runtime"), Downloads: *downloads}
	defaultDownloads := s.Downloads == ""
	if defaultDownloads {
		s.Downloads = root + "-Download"
	}
	if _, e := s.Locked(func() (any, error) { return s.Init() }); e != nil {
		panic(e)
	}
	// Created after Init so a fresh data directory is still empty when the store claims it.
	if defaultDownloads {
		_ = os.MkdirAll(s.Downloads, 0o755)
	}
	http.HandleFunc("/api/request", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" || r.Host != *addr || r.Header.Get("Origin") != "http://"+*addr {
			http.Error(w, `{"ok":false,"error":"local origin required"}`, 403)
			return
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 90001))
		var request engine.Request
		if e == nil && len(b) <= 90000 {
			request, e = engine.DecodeRequest(b)
		} else {
			e = fmt.Errorf("request too large")
		}
		var result any
		if e == nil {
			result, e = s.Locked(func() (any, error) { return s.Handle(request) })
		}
		response := map[string]any{"ok": e == nil, "data": result}
		if e != nil {
			response["error"] = e.Error()
		}
		_ = json.NewEncoder(w).Encode(response)
	})
	http.Handle("/", http.FileServer(http.Dir(*web)))
	fmt.Fprintln(os.Stdout, "Preview: http://"+*addr+"/?preview=1")
	if e := http.ListenAndServe(*addr, nil); e != nil {
		panic(e)
	}
}
