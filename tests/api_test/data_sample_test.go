package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lokey/rng-service/pkg/api"
	"github.com/lokey/rng-service/pkg/database"
)

func newMixedServer(t *testing.T, trngN, fortunaN int) (*api.Server, *database.ChannelDBHandler) {
	t.Helper()
	db, err := database.NewChannelDBHandler(trngN+8, fortunaN+8, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < trngN; i++ {
		if err := db.StoreTRNGData([]byte{byte(i >> 8), byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < fortunaN; i++ {
		if err := db.StoreFortunaData([]byte{byte((1000 + i) >> 8), byte(1000 + i)}); err != nil {
			t.Fatal(err)
		}
	}
	server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 0, prometheus.NewRegistry())
	return server, db
}

func setDataSample(t *testing.T, server *api.Server, trngRatio, fortunaRatio float64) {
	t.Helper()
	server.SetConsumeConfig(api.ConsumeConfig{
		VirtIO: true,
		DataSample: api.DataSample{
			TRNG:    api.SampleRatio{Ratio: trngRatio},
			Fortuna: api.SampleRatio{Ratio: fortunaRatio},
		},
	})
}

func postBinary(t *testing.T, server *api.Server, source string, limit int) []byte {
	t.Helper()
	body, err := json.Marshal(api.DataRequest{Format: "binary", Count: limit, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/api/v1/data", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	server.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func parseIndexes(raw []byte) []int {
	out := make([]int, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		out = append(out, int(raw[i])<<8|int(raw[i+1]))
	}
	return out
}

func TestMixedDataPhases(t *testing.T) {
	server, db := newMixedServer(t, 1000, 0)
	setDataSample(t, server, 0.1, 0)

	seen := map[int]int{}
	for lap := 0; lap < 10; lap++ {
		got := parseIndexes(postBinary(t, server, "mixed", 200))
		if len(got) != 100 {
			t.Fatalf("lap %d returned %d items", lap, len(got))
		}
		for n, idx := range got {
			if idx != lap+n*10 {
				t.Fatalf("lap %d item %d = %d", lap, n, idx)
			}
			seen[idx]++
		}
	}
	if len(seen) != 1000 {
		t.Fatalf("covered %d indexes", len(seen))
	}
	for idx, n := range seen {
		if n != 1 {
			t.Fatalf("index %d seen %d times", idx, n)
		}
	}
	if db.TRNGQueue.Size() != 1000 {
		t.Fatalf("copy changed depth to %d", db.TRNGQueue.Size())
	}
}

func TestMixedDataRemainderCoveredOnce(t *testing.T) {
	server, _ := newMixedServer(t, 1003, 0)
	setDataSample(t, server, 0.1, 0)
	got := parseIndexes(postBinary(t, server, "mixed", 1003*2))
	seen := map[int]int{}
	for _, idx := range got {
		seen[idx]++
	}
	if len(seen) != 1003 {
		t.Fatalf("covered %d indexes", len(seen))
	}
	for idx, n := range seen {
		if n != 1 || idx < 0 || idx > 1002 {
			t.Fatalf("index %d seen %d times", idx, n)
		}
	}
}

func TestMixedDataConcatenatesBothRings(t *testing.T) {
	server, db := newMixedServer(t, 20, 20)
	setDataSample(t, server, 0.5, 0.5)
	got := parseIndexes(postBinary(t, server, "mixed", 40))
	if len(got) != 20 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	for i := 0; i < 10; i++ {
		if got[i] != i*2 {
			t.Fatalf("trng[%d]=%d", i, got[i])
		}
		if got[10+i] != 1000+i*2 {
			t.Fatalf("fortuna[%d]=%d", i, got[10+i])
		}
	}
	if db.TRNGQueue.Size() != 20 || db.FortunaQueue.Size() != 20 {
		t.Fatalf("depths trng=%d fortuna=%d", db.TRNGQueue.Size(), db.FortunaQueue.Size())
	}
}

func TestMixedDataZeroRatioContributesNothing(t *testing.T) {
	server, _ := newMixedServer(t, 20, 20)
	setDataSample(t, server, 0.5, 0)
	got := parseIndexes(postBinary(t, server, "mixed", 20))
	if len(got) != 10 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	for i, idx := range got {
		if idx != i*2 {
			t.Fatalf("item %d = %d", i, idx)
		}
	}
}

func TestMixedDataConsumeRemovesSampledSlots(t *testing.T) {
	server, db := newMixedServer(t, 20, 0)
	server.SetConsumeConfig(api.ConsumeConfig{
		TRNG:   true,
		VirtIO: true,
		DataSample: api.DataSample{
			TRNG: api.SampleRatio{Ratio: 0.5},
		},
	})
	got := parseIndexes(postBinary(t, server, "mixed", 20))
	if len(got) != 10 || got[0] != 0 || got[1] != 2 || got[9] != 18 {
		t.Fatalf("consumed sample = %v", got)
	}
	if db.TRNGQueue.Size() != 10 {
		t.Fatalf("depth after consume = %d", db.TRNGQueue.Size())
	}
	left, err := db.GetTRNGData(20, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	remaining := parseIndexes(bytes.Join(left, nil))
	for i, idx := range remaining {
		if idx != i*2+1 {
			t.Fatalf("remaining = %v", remaining)
		}
	}
}

func TestMixedDataShortReadResumes(t *testing.T) {
	db, err := database.NewChannelDBHandler(16, 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := db.StoreTRNGData([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 0, prometheus.NewRegistry())
	setDataSample(t, server, 0.5, 0)
	first := postBinary(t, server, "mixed", 3)
	if !bytes.Equal(first, []byte{0, 2, 4}) {
		t.Fatalf("first = %v", first)
	}
	second := postBinary(t, server, "mixed", 3)
	if !bytes.Equal(second, []byte{6, 8, 1}) {
		t.Fatalf("second = %v", second)
	}
}

func TestDataSampleRatioChangeResetsCursor(t *testing.T) {
	server, _ := newMixedServer(t, 20, 0)
	setDataSample(t, server, 0.5, 0)
	first := parseIndexes(postBinary(t, server, "mixed", 20))
	if len(first) != 10 || first[0] != 0 || first[9] != 18 {
		t.Fatalf("first = %v", first)
	}
	setDataSample(t, server, 0.5, 0)
	second := parseIndexes(postBinary(t, server, "mixed", 20))
	if len(second) != 10 || second[0] != 1 || second[9] != 19 {
		t.Fatalf("same ratio should keep the cursor, got %v", second)
	}
	setDataSample(t, server, 1, 0)
	setDataSample(t, server, 0.5, 0)
	reset := parseIndexes(postBinary(t, server, "mixed", 20))
	if len(reset) != 10 || reset[0] != 0 || reset[9] != 18 {
		t.Fatalf("ratio change should restart at phase 0, got %v", reset)
	}
}

func TestDataSampleRatioValidation(t *testing.T) {
	server, _ := newMixedServer(t, 0, 0)
	body := []byte(`{"data_sample":{"trng":{"ratio":0.3}}}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/config/consume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	setDataSample(t, server, 0, 0.5)
	keep := []byte(`{"data_sample":{"trng":{"ratio":0.1}}}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/api/v1/config/consume", bytes.NewReader(keep))
	req.Header.Set("Content-Type", "application/json")
	server.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var cfg api.ConsumeConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DataSample.TRNG.Ratio != 0.1 || cfg.DataSample.Fortuna.Ratio != 0.5 {
		t.Fatalf("sample = %+v", cfg.DataSample)
	}
}

func TestSingleSourceIgnoresDataSample(t *testing.T) {
	db, err := database.NewChannelDBHandler(16, 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := db.StoreTRNGData([]byte{byte(10 + i)}); err != nil {
			t.Fatal(err)
		}
	}
	server := api.NewServer(db, "http://localhost:8081", "http://localhost:8082", "http://localhost:8083", 0, prometheus.NewRegistry())
	setDataSample(t, server, 0.1, 0.1)
	body, err := json.Marshal(api.DataRequest{Format: "uint8", Count: 3, Source: "trng"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/api/v1/data", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	server.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got []float64
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 10 || got[1] != 11 || got[2] != 12 {
		t.Fatalf("contiguous read = %v", got)
	}
}
