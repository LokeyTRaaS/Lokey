package atecc608a_test

import (
	"testing"

	"github.com/lokey/rng-service/pkg/atecc608a"
)

func TestEmulatedController_GenerateAndHealth(t *testing.T) {
	t.Setenv("DEVICE_ID", "test-emu")
	e := atecc608a.NewEmulatedController()

	if !e.HealthCheck() {
		t.Fatal("expected emulated health check to pass")
	}
	if e.Mode() != "emulated" {
		t.Fatalf("mode=%q want emulated", e.Mode())
	}
	if e.DeviceID() != "test-emu" {
		t.Fatalf("device_id=%q want test-emu", e.DeviceID())
	}

	data, err := e.GenerateRandom()
	if err != nil {
		t.Fatalf("GenerateRandom: %v", err)
	}
	if len(data) != 32 {
		t.Fatalf("len=%d want 32", len(data))
	}

	data2, err := e.GenerateRandom()
	if err != nil {
		t.Fatalf("GenerateRandom second: %v", err)
	}
	same := true
	for i := range data {
		if data[i] != data2[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("expected distinct random samples from emulator")
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
