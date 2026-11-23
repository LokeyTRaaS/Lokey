package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
)

func TestFeedRatioTakesTenPercentAndCanConsume(t *testing.T) {
	db, err := database.NewChannelDBHandler(20, 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	var posted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/seed" {
			posted++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"seeded"}`))
	}))
	defer srv.Close()

	server := api.NewServer(db, "http://unused", srv.URL, srv.URL, 0, prometheus.NewRegistry())
	server.SetConsumeConfig(api.ConsumeConfig{
		VirtIO:      true,
		FortunaSeed: api.FeedSource{Ratio: 0.1, Consume: false},
		VirtIOSources: api.VirtIOSources{
			TRNG:    api.FeedSource{Ratio: 0.1, Consume: true},
			Fortuna: api.FeedSource{Ratio: 0.5, Consume: false},
		},
	})

	for i := 0; i < 10; i++ {
		sample := []byte{byte(i), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, byte(31 - i)}
		if err := db.StoreTRNGData(sample); err != nil {
			t.Fatal(err)
		}
		server.ObserveTRNGSample(sample)
	}

	stats, err := db.GetDetailedStats()
	if err != nil {
		t.Fatal(err)
	}
	// VirtIO consumes its 10 percent. Fortuna's reseed copies, so it does not remove another item.
	if stats.TRNG.QueueCurrent != 9 {
		t.Fatalf("TRNG ring current=%d, want 9", stats.TRNG.QueueCurrent)
	}

	if err := server.SeedFortuna(); err != nil {
		t.Fatal(err)
	}
	if err := server.SeedVirtIOFromTRNG(); err != nil {
		t.Fatal(err)
	}
	if posted != 2 {
		t.Fatalf("posts=%d, want one Fortuna reseed and one VirtIO fill", posted)
	}

	cfg := api.ConsumeConfig{}
	// A later PUT of only the Fortuna ratio must leave the VirtIO draws alone.
	body := []byte(`{"fortuna_seed":{"ratio":0.5}}`)
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/config/consume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update consume: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.FortunaSeed.Ratio != 0.5 || cfg.FortunaSeed.Consume {
		t.Fatalf("fortuna seed = %+v", cfg.FortunaSeed)
	}
	if cfg.VirtIOSources.TRNG.Ratio != 0.1 || !cfg.VirtIOSources.TRNG.Consume {
		t.Fatalf("virtio trng draw changed: %+v", cfg.VirtIOSources.TRNG)
	}
	if cfg.VirtIOSources.Fortuna.Ratio != 0.5 || cfg.VirtIOSources.Fortuna.Consume {
		t.Fatalf("virtio fortuna draw changed: %+v", cfg.VirtIOSources.Fortuna)
	}
	if cfg.TRNG || cfg.Fortuna || !cfg.VirtIO {
		t.Fatalf("client flags changed: %+v", cfg)
	}
}
