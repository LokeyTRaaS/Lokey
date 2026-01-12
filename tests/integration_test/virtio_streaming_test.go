package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVirtIOService_StreamEndpoint(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	// Seed some data
	testData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	for i := 0; i < 5; i++ {
		service.device.Seed(testData)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream?chunk_size=32&max_bytes=64", nil)
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	// Check headers
	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("Expected Content-Type application/octet-stream, got %s", w.Header().Get("Content-Type"))
	}

	if w.Header().Get("Transfer-Encoding") != "chunked" {
		t.Errorf("Expected Transfer-Encoding chunked, got %s", w.Header().Get("Transfer-Encoding"))
	}

	// Read body (should have at least some data)
	body, err := io.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if len(body) == 0 {
		t.Error("Expected non-empty response body")
	}

	// Note: Due to chunk-based streaming, we might get slightly more than max_bytes
	// (up to one chunk size), which is acceptable behavior
	if len(body) > 64+32 {
		t.Errorf("Expected max ~96 bytes (64 + chunk_size), got %d", len(body))
	}
}

func TestVirtIOService_StreamEndpoint_NoMaxBytes(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	// Seed some data
	testData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	for i := 0; i < 3; i++ {
		service.device.Seed(testData)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream?chunk_size=32", nil)
	service.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	// Should receive at least some data
	body, err := io.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if len(body) == 0 {
		t.Error("Expected non-empty response body")
	}
}

func TestVirtIOService_StreamEndpoint_InvalidChunkSize(t *testing.T) {
	service, cleanup := setupVirtIOServiceForTest(t, 100)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stream?chunk_size=invalid", nil)
	service.router.ServeHTTP(w, req)

	// Should still work (uses default chunk size)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}
