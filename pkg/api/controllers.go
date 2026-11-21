package api

import (
	"fmt"
	"strings"
)

// ControllerEndpoint is a named TRNG controller HTTP base URL.
type ControllerEndpoint struct {
	DeviceID string
	Addr     string
}

// ParseControllerAddrs parses CONTROLLER_ADDRS as "id=http://host:port,id2=http://..."
// or falls back to a single CONTROLLER_ADDR as device "default".
func ParseControllerAddrs(addrsCSV, fallbackAddr string) ([]ControllerEndpoint, error) {
	addrsCSV = strings.TrimSpace(addrsCSV)
	if addrsCSV == "" {
		fallbackAddr = strings.TrimSpace(fallbackAddr)
		if fallbackAddr == "" {
			return nil, fmt.Errorf("no CONTROLLER_ADDRS or CONTROLLER_ADDR configured")
		}
		return []ControllerEndpoint{{DeviceID: "default", Addr: strings.TrimRight(fallbackAddr, "/")}}, nil
	}

	parts := strings.Split(addrsCSV, ",")
	out := make([]ControllerEndpoint, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, addr, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid CONTROLLER_ADDRS entry %q (want id=url)", part)
		}
		id = strings.TrimSpace(id)
		addr = strings.TrimRight(strings.TrimSpace(addr), "/")
		if id == "" || addr == "" {
			return nil, fmt.Errorf("invalid CONTROLLER_ADDRS entry %q", part)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate device_id %q in CONTROLLER_ADDRS", id)
		}
		seen[id] = struct{}{}
		out = append(out, ControllerEndpoint{DeviceID: id, Addr: addr})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CONTROLLER_ADDRS is empty")
	}
	return out, nil
}
