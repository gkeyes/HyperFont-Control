package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hyperfont.local/control/internal/engine"
	"io"
	"os"
)

func main() {
	response := map[string]any{"ok": false}
	code := 0
	defer func() {
		if x := recover(); x != nil {
			response = map[string]any{"ok": false, "error": fmt.Sprint(x)}
			code = 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(response)
		os.Exit(code)
	}()
	s := &engine.Store{Root: "/data/adb/hyperfont-control", RomRoot: "/", Runtime: "/dev/hyperfont-control"}
	args := os.Args[1:]
	if len(args) == 0 {
		response["error"] = "需要操作参数"
		code = 1
		return
	}
	var r engine.Request
	var e error
	var result any
	if args[0] == "probe" {
		result, e = engine.Probe()
	} else if args[0] == "probe-mount" {
		result, e = s.Locked(func() (any, error) { return s.ProbeMount() })
	} else if args[0] == "boot" {
		result, e = s.Locked(func() (any, error) { return s.Boot() })
	} else if args[0] == "boot-complete" {
		result, e = s.Locked(func() (any, error) { return s.ConfirmBoot() })
	} else if args[0] == "rebuild" {
		result, e = s.Locked(func() (any, error) { return s.RebuildCommitted() })
	} else {
		var raw []byte
		if args[0] == "init" {
			r.Op = "init"
		} else if args[0] == "request" && len(args) == 2 && len(args[1]) < 120000 {
			raw, e = base64.StdEncoding.DecodeString(args[1])
		} else if args[0] == "stdin" {
			raw, e = io.ReadAll(io.LimitReader(os.Stdin, 90001))
			if len(raw) > 90000 {
				e = fmt.Errorf("请求过大")
			}
		} else {
			e = fmt.Errorf("未知命令")
		}
		if e == nil && raw != nil {
			r, e = engine.DecodeRequest(raw)
		}
		if e == nil {
			result, e = s.Locked(func() (any, error) { return s.Handle(r) })
		}
	}
	if e != nil {
		response["error"] = e.Error()
		code = 1
	} else {
		response = map[string]any{"ok": true, "data": result}
	}
}
