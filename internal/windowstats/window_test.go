package windowstats

import "testing"

func TestWindowStats(t *testing.T) {
	w := NewWindowStats(3)
	w.Push(1)
	w.Push(2)
	w.Push(3)

	if w.Size() != 3 {
		t.Fatalf("expected count 3, got %d", w.Size())
	}
	if w.Average() != 2 {
		t.Fatalf("expected mean 2, got %f", w.Average())
	}

	w.Push(4)
	if w.Size() != 3 {
		t.Fatalf("expected count 3 after rollover, got %d", w.Size())
	}
	if w.Average() != 3 {
		t.Fatalf("expected mean 3 after rollover, got %f", w.Average())
	}
}
