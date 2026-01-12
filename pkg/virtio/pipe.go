// Package virtio provides named pipe (FIFO) support for VirtIO RNG.
// Named pipes allow hypervisors (QEMU, Proxmox, etc.) to read random data
// directly from the filesystem, providing high-throughput access.
package virtio

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	// DefaultPipePath is the default path for the named pipe
	DefaultPipePath = "/var/run/lokey/virtio-rng"
	// PipeWriteChunkSize is the size of chunks written to the pipe
	PipeWriteChunkSize = 32 // Match RandomDataSize
)

// NamedPipe represents a named pipe (FIFO) for streaming random data
type NamedPipe struct {
	path   string
	file   *os.File
	mu     sync.Mutex
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewNamedPipe creates a new named pipe at the specified path
func NewNamedPipe(path string) (*NamedPipe, error) {
	if path == "" {
		path = DefaultPipePath
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pipe directory: %w", err)
	}

	// Remove existing pipe if it exists (in case of previous crash)
	if _, err := os.Stat(path); err == nil {
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("failed to remove existing pipe: %w", err)
		}
	}

	// Create FIFO (named pipe)
	if err := syscall.Mkfifo(path, 0666); err != nil {
		return nil, fmt.Errorf("failed to create named pipe: %w", err)
	}

	return &NamedPipe{
		path:   path,
		stopCh: make(chan struct{}),
	}, nil
}

// Start begins writing data from the device queue to the pipe
func (p *NamedPipe) Start(device *VirtIORNG) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Start background goroutine to write data
	// The goroutine will handle opening the pipe (which blocks until reader opens it)
	p.wg.Add(1)
	go p.writeLoop(device)

	return nil
}

// writeLoop continuously reads from the device queue and writes to the pipe
func (p *NamedPipe) writeLoop(device *VirtIORNG) {
	defer p.wg.Done()

	// Open pipe in write-only mode (will block until reader opens it)
	// This is done in the goroutine so Start() doesn't block
	p.mu.Lock()
	file, err := os.OpenFile(p.path, os.O_WRONLY, 0)
	if err != nil {
		p.mu.Unlock()
		log.Printf("[ERROR] Failed to open pipe for writing: %v", err)
		return
	}
	p.file = file
	p.mu.Unlock()

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
		log.Printf("[INFO] Named pipe opened at %s", p.path)
	}

	ticker := time.NewTicker(10 * time.Millisecond) // Check queue every 10ms
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			// Try to pull data from queue (non-blocking check)
			queueCurrent := device.GetQueueCurrent()
			if queueCurrent == 0 {
				continue // No data available, wait for next tick
			}

			// Pull a chunk of data (32 bytes)
			data, _, err := device.PullBytes(PipeWriteChunkSize)
			if err != nil {
				// Queue might be empty now, continue
				continue
			}

			// Write to pipe (this will block if pipe buffer is full, which is desired)
			if err := p.Write(data); err != nil {
				// Handle EPIPE (broken pipe - reader closed)
				if err == io.ErrClosedPipe || isEPIPE(err) {
					logLevel := os.Getenv("LOG_LEVEL")
					if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
						log.Printf("[INFO] Pipe reader closed, waiting for new reader...")
					}
					// Close current file handle
					p.mu.Lock()
					if p.file != nil {
						p.file.Close()
						p.file = nil
					}
					p.mu.Unlock()

					// Try to reopen pipe (will block until new reader opens it)
					for {
						select {
						case <-p.stopCh:
							return
						case <-time.After(1 * time.Second):
							p.mu.Lock()
							if p.file == nil {
								file, err := os.OpenFile(p.path, os.O_WRONLY, 0)
								if err == nil {
									p.file = file
									logLevel := os.Getenv("LOG_LEVEL")
									if logLevel == "DEBUG" || logLevel == "INFO" || logLevel == "" {
										log.Printf("[INFO] Named pipe reopened at %s", p.path)
									}
									p.mu.Unlock()
									break
								}
							}
							p.mu.Unlock()
						}
					}
				} else {
					log.Printf("[WARN] Error writing to pipe: %v", err)
				}
			}
		}
	}
}

// Write writes data to the pipe (thread-safe)
func (p *NamedPipe) Write(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.file == nil {
		return io.ErrClosedPipe
	}

	_, err := p.file.Write(data)
	return err
}

// Stop gracefully stops the named pipe
func (p *NamedPipe) Stop() error {
	p.mu.Lock()
	
	// Check if already stopped
	select {
	case <-p.stopCh:
		// Already stopped
		p.mu.Unlock()
		return nil
	default:
		// Signal stop
		close(p.stopCh)
	}

	// Close file handle
	if p.file != nil {
		if err := p.file.Close(); err != nil {
			log.Printf("[WARN] Error closing pipe file: %v", err)
		}
		p.file = nil
	}
	p.mu.Unlock()

	// Wait for goroutine to finish
	p.wg.Wait()

	// Optionally remove pipe file (comment out if you want to keep it for hypervisor access)
	// if err := os.Remove(p.path); err != nil {
	// 	log.Printf("[WARN] Error removing pipe file: %v", err)
	// }

	return nil
}

// Path returns the pipe filesystem path
func (p *NamedPipe) Path() string {
	return p.path
}

// isEPIPE checks if an error is EPIPE (broken pipe)
func isEPIPE(err error) bool {
	if err == nil {
		return false
	}
	if pathErr, ok := err.(*os.PathError); ok {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			return errno == syscall.EPIPE
		}
	}
	return false
}
