package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
)

func TestControllerStatus_EmulatorFromHealth(t *testing.T) {
	hw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"mode":      "hardware",
			"device_id": "device_a",
			"emulator":  false,
		})
	}))
	defer hw.Close()

	emu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"mode":      "emulated",
			"device_id": "device_b",
			"emulator":  true,
		})
	}))
	defer emu.Close()

	db, err := database.NewChannelDBHandler(10, 10, 10)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer db.Close()

	t.Setenv("CONTROLLER_ADDRS", "device_a="+hw.URL+",device_b="+emu.URL)
	server := api.NewServer(db, hw.URL, "http://127.0.0.1:9", "http://127.0.0.1:9", 0, prometheus.NewRegistry())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/status", nil)
	server.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", w.Code, w.Body.String())
	}

	var resp api.StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	a, ok := resp.Controllers["device_a"]
	if !ok || !a.Healthy || a.Emulator == nil || *a.Emulator {
		t.Fatalf("device_a=%+v ok=%v want healthy hardware", a, ok)
	}
	b, ok := resp.Controllers["device_b"]
	if !ok || !b.Healthy || b.Emulator == nil || !*b.Emulator {
		t.Fatalf("device_b=%+v ok=%v want healthy emulator", b, ok)
	}
}

func TestControllerStatus_LastKnownEmulatorOnFailure(t *testing.T) {
	healthy := true
	ctrl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "unhealthy",
				"mode":      "emulated",
				"device_id": "device_a",
				"emulator":  true,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"mode":      "emulated",
			"device_id": "device_a",
			"emulator":  true,
		})
	}))
	defer ctrl.Close()

	db, err := database.NewChannelDBHandler(10, 10, 10)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer db.Close()

	t.Setenv("CONTROLLER_ADDRS", "device_a="+ctrl.URL)
	server := api.NewServer(db, ctrl.URL, "http://127.0.0.1:9", "http://127.0.0.1:9", 0, prometheus.NewRegistry())

	getStatus := func() api.StatusResponse {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/status", nil)
		server.Router.ServeHTTP(w, req)
		var resp api.StatusResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp
	}

	first := getStatus()
	st := first.Controllers["device_a"]
	if !st.Healthy || st.Emulator == nil || !*st.Emulator {
		t.Fatalf("first probe=%+v", st)
	}

	healthy = false
	second := getStatus()
	st = second.Controllers["device_a"]
	if st.Healthy {
		t.Fatalf("expected unhealthy after failure, got %+v", st)
	}
	if st.Emulator == nil || !*st.Emulator {
		t.Fatalf("expected last-known emulator=true, got %+v", st)
	}

	// Completely unreachable: still keep last-known emulator.
	ctrl.Close()
	third := getStatus()
	st = third.Controllers["device_a"]
	if st.Healthy {
		t.Fatalf("expected unhealthy when unreachable, got %+v", st)
	}
	if st.Emulator == nil || !*st.Emulator {
		t.Fatalf("expected cached emulator=true when unreachable, got %+v", st)
	}
}
