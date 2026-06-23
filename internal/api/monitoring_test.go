package api

import (
	"testing"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

func TestDownsampleMetrics(t *testing.T) {
	// Fewer than the cap: returned unchanged.
	small := make([]models.MetricSample, 10)
	if got := downsampleMetrics(small, 96); len(got) != 10 {
		t.Errorf("len = %d, want 10 (unchanged)", len(got))
	}

	// 1000 samples aggregate to <= cap, and averaging is correct.
	in := make([]models.MetricSample, 1000)
	base := time.Now()
	for i := range in {
		in[i] = models.MetricSample{Ts: base.Add(time.Duration(i) * time.Minute), CPU: 50, Mem: 20, Disk: 10}
	}
	out := downsampleMetrics(in, 96)
	if len(out) == 0 || len(out) > 96 {
		t.Fatalf("len = %d, want 1..96", len(out))
	}
	for _, m := range out {
		if m.CPU != 50 || m.Mem != 20 || m.Disk != 10 {
			t.Errorf("averaged constant series wrong: %+v", m)
		}
	}
	// Last bucket keeps the final timestamp.
	if !out[len(out)-1].Ts.Equal(in[len(in)-1].Ts) {
		t.Errorf("last bucket ts = %v, want %v", out[len(out)-1].Ts, in[len(in)-1].Ts)
	}
}
