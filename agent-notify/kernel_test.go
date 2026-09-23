package agentnotify_test

import (
	"slices"
	"testing"

	an "github.com/lassoColombo/agent-notify"
)

// TestRanksOrderByWhatYourDelayCosts pins the one judgement in the table:
// blocked-on-you outranks broke, because a blocked agent is still burning your
// wall-clock and a broken one has already stopped.
func TestRanksOrderByWhatYourDelayCosts(t *testing.T) {
	ordered := an.Kernels()
	if len(ordered) != 6 {
		t.Fatalf("Kernels() has %d entries, want the 6 of §A5.6", len(ordered))
	}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].Rank() <= ordered[i].Rank() {
			t.Errorf("%q (%d) does not outrank %q (%d)",
				ordered[i-1], ordered[i-1].Rank(), ordered[i], ordered[i].Rank())
		}
	}
	if an.BlockedOnYou.Rank() <= an.Broke.Rank() {
		t.Error("blocked-on-you must outrank broke")
	}
	if an.Ended.Rank() != 0 {
		t.Errorf("ended rank = %d, want 0", an.Ended.Rank())
	}
}

// TestRanksAreSparse is what lets a seventh state be inserted without
// renumbering neighbours, which would break every display keyed on the rank.
func TestRanksAreSparse(t *testing.T) {
	ordered := an.Kernels()
	for i := 1; i < len(ordered); i++ {
		if gap := ordered[i-1].Rank() - ordered[i].Rank(); gap < 5 {
			t.Errorf("only %d between %q and %q: nothing can be inserted there",
				gap, ordered[i-1], ordered[i])
		}
	}
}

func TestUnknownKernel(t *testing.T) {
	unknown := an.Kernel("hibernating")
	if unknown.Known() {
		t.Error("an invented kernel reported itself as known")
	}
	if unknown.Rank() != an.RankUnknown {
		t.Errorf("Rank() = %d, want RankUnknown — the record's own rank is the one to use", unknown.Rank())
	}
	if !unknown.Live() {
		t.Error("an unknown kernel must be live (R5)")
	}
}

func TestLiveness(t *testing.T) {
	live := []an.Kernel{an.BlockedOnYou, an.Broke, an.FinishedATurn, an.Working, an.Idle}
	for _, kernel := range live {
		if !kernel.Live() {
			t.Errorf("%q should be live", kernel)
		}
	}
	for _, kernel := range []an.Kernel{an.Ended, ""} {
		if kernel.Live() {
			t.Errorf("%q should not be live", kernel)
		}
	}
	if slices.Contains(live, an.Ended) {
		t.Error("the fixture is wrong")
	}
}
