package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"
)

// ControllerEndpoint is a named TRNG controller HTTP base URL.
type ControllerEndpoint struct {
	DeviceID string
	Addr     string
}

// ControllerStatus is the per-device view exposed on /status and /health.
// Emulator is omitted (nil) until a successful health probe has observed the controller.
type ControllerStatus struct {
	Healthy  bool  `json:"healthy"`
	Emulator *bool `json:"emulator,omitempty"`
}

// ControllerHealth is the decoded controller /health payload (plus reachability).
type ControllerHealth struct {
	Healthy   bool
	Emulator  bool
	Mode      string
	DeviceID  string
	Reachable bool
}

type controllerLastKnown struct {
	Emulator bool
	Mode     string
	SeenAt   time.Time
}

type controllerHealthPayload struct {
	Status   string `json:"status"`
	Mode     string `json:"mode"`
	DeviceID string `json:"device_id"`
	Emulator *bool  `json:"emulator"`
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

// probeControllerHealth GETs addr/health and decodes the controller JSON body.
// Fail closed: unreachable, non-2xx without a parseable body, or unparseable body ⇒ not healthy.
func (s *Server) probeControllerHealth(addr string) ControllerHealth {
	out := ControllerHealth{}
	url := strings.TrimRight(addr, "/") + "/health"
	resp, err := s.httpClient.Get(url) // #nosec G107 -- addr from server config
	if err != nil {
		return out
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Warning: failed to close controller health response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out
	}

	var payload controllerHealthPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		// Legacy / non-controller health: fall back to status code only.
		out.Reachable = resp.StatusCode >= 200 && resp.StatusCode < 300
		out.Healthy = out.Reachable
		return out
	}

	out.Reachable = true
	out.Mode = payload.Mode
	out.DeviceID = payload.DeviceID
	if payload.Emulator != nil {
		out.Emulator = *payload.Emulator
	} else {
		out.Emulator = strings.EqualFold(payload.Mode, "emulated")
	}
	out.Healthy = resp.StatusCode >= 200 && resp.StatusCode < 300 &&
		strings.EqualFold(payload.Status, "healthy")
	return out
}

// collectControllerStatuses probes every configured controller and updates last-known cache.
func (s *Server) collectControllerStatuses() map[string]ControllerStatus {
	result := make(map[string]ControllerStatus, len(s.controllersOrDefault()))
	for _, ep := range s.controllersOrDefault() {
		result[ep.DeviceID] = s.statusForController(ep)
	}
	return result
}

func (s *Server) statusForController(ep ControllerEndpoint) ControllerStatus {
	health := s.probeControllerHealth(ep.Addr)
	st := ControllerStatus{Healthy: health.Healthy}

	// Structured controller health includes mode and/or device_id from the payload.
	structured := health.Reachable && (health.Mode != "" || health.DeviceID != "")
	if structured {
		if health.DeviceID != "" && health.DeviceID != ep.DeviceID {
			log.Printf("[WARN] controller at %s reported device_id=%q, configured as %q",
				ep.Addr, health.DeviceID, ep.DeviceID)
		}
		s.rememberController(ep.DeviceID, health)
		emu := health.Emulator
		st.Emulator = &emu
		return st
	}

	// Unreachable or opaque health: keep last-known emulator when available.
	if known, ok := s.lastKnownController(ep.DeviceID); ok {
		emu := known.Emulator
		st.Emulator = &emu
	}
	return st
}

func (s *Server) rememberController(deviceID string, health ControllerHealth) {
	s.controllerMetaMu.Lock()
	defer s.controllerMetaMu.Unlock()
	if s.controllerLastKnown == nil {
		s.controllerLastKnown = make(map[string]controllerLastKnown)
	}
	s.controllerLastKnown[deviceID] = controllerLastKnown{
		Emulator: health.Emulator,
		Mode:     health.Mode,
		SeenAt:   time.Now().UTC(),
	}
}

func (s *Server) lastKnownController(deviceID string) (controllerLastKnown, bool) {
	s.controllerMetaMu.RLock()
	defer s.controllerMetaMu.RUnlock()
	if s.controllerLastKnown == nil {
		return controllerLastKnown{}, false
	}
	v, ok := s.controllerLastKnown[deviceID]
	return v, ok
}
