package pipeline

import "testing"

func TestShouldCorruptRules(t *testing.T) {
	c := &BCoordinator{}

	c.SetErrorInjection(false, 100)
	if c.shouldCorrupt(0) {
		t.Fatalf("expected disabled injection to never corrupt")
	}

	c.SetErrorInjection(true, 1)
	if c.shouldCorrupt(0) {
		t.Fatalf("expected rps=1 to never corrupt")
	}

	c.SetErrorInjection(true, 5)
	for i := 0; i < 5; i++ {
		want := i == 0
		if got := c.shouldCorrupt(i); got != want {
			t.Fatalf("rps=5 idx=%d expected %v got %v", i, want, got)
		}
	}

	c.SetErrorInjection(true, 10)
	for i := 0; i < 10; i++ {
		want := i == 0
		if got := c.shouldCorrupt(i); got != want {
			t.Fatalf("rps=10 idx=%d expected %v got %v", i, want, got)
		}
	}

	c.SetErrorInjection(true, 23)
	corruptCount := 0
	for i := 0; i < 23; i++ {
		if c.shouldCorrupt(i) {
			corruptCount++
		}
	}
	if corruptCount != 2 {
		t.Fatalf("rps=23 expected 2 corrupt payloads, got %d", corruptCount)
	}
}
