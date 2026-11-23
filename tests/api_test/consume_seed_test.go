package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
)

func TestSeedVirtIOHonorsPerSourceConsume(t *testing.T) {
	db, err := database.NewChannelDBHandler(10, 10, 10)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	seedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seed" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"seeded","count":1}`))
	}))
	defer seedServer.Close()

	server := api.NewServer(db, "http://unused", "http://unused", seedServer.URL, 0, prometheus.NewRegistry())
	// Varied sample so TRNG seed quality validation accepts it.
	trngSample := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	if err := db.StoreTRNGData(trngSample); err != nil {
		t.Fatalf("store trng: %v", err)
	}
	if err := db.StoreFortunaData([]byte{9, 8, 7, 6, 5}); err != nil {
		t.Fatalf("store fortuna: %v", err)
	}

	server.SetConsumeConfig(api.ConsumeConfig{
		VirtIO: true,
		VirtIOSources: api.VirtIOSources{
			TRNG:    api.FeedSource{Ratio: 1, Consume: false},
			Fortuna: api.FeedSource{Ratio: 1, Consume: true},
		},
	})
	server.ObserveTRNGSample(trngSample)
	if err := server.SeedVirtIOFromTRNG(); err != nil {
		t.Fatalf("seed trng copy: %v", err)
	}
	stats, err := db.GetDetailedStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.TRNG.QueueCurrent != 1 {
		t.Fatalf("TRNG copy consumed the ring: current=%d", stats.TRNG.QueueCurrent)
	}
	if stats.Fortuna.QueueCurrent != 1 {
		t.Fatalf("Fortuna ring changed during TRNG seed: current=%d", stats.Fortuna.QueueCurrent)
	}

	server.ObserveFortunaSample([]byte{9, 8, 7, 6, 5})
	stats, err = db.GetDetailedStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Fortuna.QueueCurrent != 0 {
		t.Fatalf("Fortuna consume left the chosen pull in the ring: current=%d", stats.Fortuna.QueueCurrent)
	}
	if err := server.SeedVirtIOFromFortuna(); err != nil {
		t.Fatalf("seed fortuna consume: %v", err)
	}
	stats, err = db.GetDetailedStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Fortuna.QueueCurrent != 0 {
		t.Fatalf("Fortuna consume left items behind: current=%d", stats.Fortuna.QueueCurrent)
	}
	if stats.TRNG.QueueCurrent != 1 {
		t.Fatalf("TRNG ring changed during Fortuna seed: current=%d", stats.TRNG.QueueCurrent)
	}
}
