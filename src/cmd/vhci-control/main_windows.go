//go:build windows

// Bounded lab CLI for the native VHCI package. Not yet wired into product GUI.
package main

import (
	"context"
	"debug/pe"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"virtualcables/internal/vhci"
)

func run() (any, error) {
	action := flag.String("action", "list", "list, attach, detach or imports")
	bus := flag.String("bus", "1-1", "local experimental cable bus ID")
	port := flag.Uint("port", 0, "explicit owned port for detach")
	low := flag.Bool("low-latency", false, "usbip receive mode (not Coemorph product mode)")
	flag.Parse()
	if flag.NArg() != 0 {
		return nil, fmt.Errorf("unexpected positional arguments")
	}
	if *action == "imports" {
		path, err := os.Executable()
		if err != nil {
			return nil, err
		}
		f, err := pe.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		// debug/pe.ImportedLibraries is still a TODO in the pinned Go runtime.
		symbols, err := f.ImportedSymbols()
		if err != nil {
			return nil, err
		}
		libraries := map[string]bool{}
		for _, symbol := range symbols {
			i := strings.LastIndexByte(symbol, ':')
			if i < 0 {
				return nil, fmt.Errorf("invalid imported symbol %q", symbol)
			}
			libraries[strings.ToLower(symbol[i+1:])] = true
		}
		if len(libraries) == 0 {
			return nil, fmt.Errorf("no PE import evidence")
		}
		var result []string
		for library := range libraries {
			result = append(result, library)
		}
		sort.Strings(result)
		return result, nil
	}
	if *action != "list" && *action != "attach" && *action != "detach" {
		return nil, fmt.Errorf("invalid action")
	}
	c, err := vhci.Open()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	switch *action {
	case "list":
		return c.List(ctx)
	case "attach":
		p, err := c.AttachLocalOnce(ctx, *bus, *low)
		return struct {
			Port uint32 `json:"port"`
		}{p}, err
	case "detach":
		if *port == 0 || uint64(*port) > 0x7fffffff {
			return nil, fmt.Errorf("invalid explicit port")
		}
		devices, err := c.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range devices {
			if d.Port == uint32(*port) && d.BusID == *bus {
				if err := c.DetachVerified(ctx, d); err != nil {
					return nil, err
				}
				return struct {
					Detached uint32 `json:"detached_port"`
				}{d.Port}, nil
			}
		}
		return nil, fmt.Errorf("owned port/bus not found")
	}
	return nil, fmt.Errorf("unreachable action")
}

func main() {
	result, err := run()
	message := ""
	if err != nil {
		message = err.Error()
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(struct {
		Result any    `json:"result"`
		Error  string `json:"error"`
	}{result, message}); encodeErr != nil {
		os.Exit(2)
	}
	if err != nil {
		os.Exit(1)
	}
}
