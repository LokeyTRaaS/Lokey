package virtio_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lokey/rng-service/pkg/virtio"
)

func TestNamedPipe_Creation(t *testing.T) {
	tmpDir := t.TempDir()
	pipePath := filepath.Join(tmpDir, "test-pipe")

	pipe, err := virtio.NewNamedPipe(pipePath)
	if err != nil {
		t.Fatalf("Failed to create named pipe: %v", err)
	}
	defer pipe.Stop()

	// Check pipe file exists
	if _, err := os.Stat(pipePath); os.IsNotExist(err) {
		t.Error("Pipe file was not created")
	}

	// Check pipe path
	if pipe.Path() != pipePath {
		t.Errorf("Expected pipe path %s, got %s", pipePath, pipe.Path())
	}
}

func TestNamedPipe_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	pipePath := filepath.Join(tmpDir, "test-pipe")

	pipe, err := virtio.NewNamedPipe(pipePath)
	if err != nil {
		t.Fatalf("Failed to create named pipe: %v", err)
	}

	// Test that Stop() works even if pipe was never started
	if err := pipe.Stop(); err != nil {
		t.Errorf("Failed to stop pipe (never started): %v", err)
	}

	// Verify pipe can be stopped multiple times without error
	if err := pipe.Stop(); err != nil {
		t.Errorf("Second stop should be idempotent: %v", err)
	}
}

