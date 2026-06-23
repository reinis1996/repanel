package system

import (
	"strings"
	"testing"
)

func TestSliceLimitLines(t *testing.T) {
	got := sliceLimitLines(AccountLimits{CPUQuotaPct: 150, MemoryMaxMB: 512, ProcessesMax: 200})
	for _, want := range []string{"CPUQuota=150%", "MemoryMax=512M", "TasksMax=200"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Unset limits are omitted entirely (no zero/garbage lines).
	partial := sliceLimitLines(AccountLimits{MemoryMaxMB: 256})
	if strings.Contains(partial, "CPUQuota") || strings.Contains(partial, "TasksMax") || !strings.Contains(partial, "MemoryMax=256M") {
		t.Errorf("partial limits wrong:\n%s", partial)
	}
	if sliceLimitLines(AccountLimits{}) != "" {
		t.Error("empty limits should render nothing")
	}
}

func TestAccountSliceName(t *testing.T) {
	if got := AccountSliceName(42); got != "repanel-acct-42.slice" {
		t.Errorf("AccountSliceName(42) = %q", got)
	}
}

func TestAccountLimitsEmpty(t *testing.T) {
	if !(AccountLimits{}).empty() {
		t.Error("zero AccountLimits should be empty")
	}
	if (AccountLimits{CPUQuotaPct: 1}).empty() {
		t.Error("non-zero AccountLimits should not be empty")
	}
}
