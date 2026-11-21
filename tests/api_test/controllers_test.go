package api_test

import (
	"testing"

	"github.com/lokey/rng-service/pkg/api"
)

func TestParseControllerAddrs(t *testing.T) {
	eps, err := api.ParseControllerAddrs("a=http://controller-a:8081,b=http://controller-b:8081", "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("len=%d", len(eps))
	}
	if eps[0].DeviceID != "a" || eps[0].Addr != "http://controller-a:8081" {
		t.Fatalf("eps[0]=%+v", eps[0])
	}

	eps, err = api.ParseControllerAddrs("", "http://controller:8081/")
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if len(eps) != 1 || eps[0].DeviceID != "default" || eps[0].Addr != "http://controller:8081" {
		t.Fatalf("fallback eps=%+v", eps)
	}

	if _, err := api.ParseControllerAddrs("bad", ""); err == nil {
		t.Fatal("expected error for bad entry")
	}
}
