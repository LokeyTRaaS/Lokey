package fortuna

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newTestGenerator(t *testing.T) *Generator {
	t.Helper()
	seed := make([]byte, MinimumSeedLength)
	if _, err := rand.Read(seed); err != nil {
		t.Fatalf("failed to generate seed: %v", err)
	}
	g, err := NewGenerator(seed)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}
	return g
}

func TestKeyRotatesAfterGenerate(t *testing.T) {
	g := newTestGenerator(t)

	keyBefore := make([]byte, len(g.key))
	copy(keyBefore, g.key)

	if _, err := g.GenerateRandomData(16); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	if bytes.Equal(keyBefore, g.key) {
		t.Error("key was not rotated after generating data")
	}
}

func TestGenerateRandomDataIsUnique(t *testing.T) {
	g := newTestGenerator(t)

	first, err := g.GenerateRandomData(32)
	if err != nil {
		t.Fatalf("first generate failed: %v", err)
	}
	second, err := g.GenerateRandomData(32)
	if err != nil {
		t.Fatalf("second generate failed: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Error("consecutive generate calls returned identical output")
	}
}

func TestReseedChangesOutput(t *testing.T) {
	seed := make([]byte, MinimumSeedLength)
	if _, err := rand.Read(seed); err != nil {
		t.Fatalf("failed to generate seed: %v", err)
	}

	g1, err := NewGenerator(seed)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}
	g2, err := NewGenerator(seed)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	reseed := make([]byte, 32)
	if _, err := rand.Read(reseed); err != nil {
		t.Fatalf("failed to generate reseed material: %v", err)
	}
	if err := g2.Reseed([][]byte{reseed}); err != nil {
		t.Fatalf("reseed failed: %v", err)
	}

	out1, err := g1.GenerateRandomData(32)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	out2, err := g2.GenerateRandomData(32)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	if bytes.Equal(out1, out2) {
		t.Error("reseeded generator produced same output as original")
	}
}
