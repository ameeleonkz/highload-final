package windowstats

import "math"

// WindowStats keeps a fixed-size window of values and provides mean/std dev.
type WindowStats struct {
	capacity   int
	values     []float64
	position   int
	samples    int
	sum        float64
	sumSquares float64
}

func NewWindowStats(capacity int) *WindowStats {
	if capacity <= 0 {
		capacity = 1
	}
	return &WindowStats{
		capacity: capacity,
		values:   make([]float64, capacity),
	}
}

func (w *WindowStats) Push(v float64) {
	if w.samples < w.capacity {
		w.values[w.position] = v
		w.sum += v
		w.sumSquares += v * v
		w.position = (w.position + 1) % w.capacity
		w.samples++
		return
	}

	old := w.values[w.position]
	w.sum -= old
	w.sumSquares -= old * old

	w.values[w.position] = v
	w.sum += v
	w.sumSquares += v * v
	w.position = (w.position + 1) % w.capacity
}

func (w *WindowStats) Size() int {
	return w.samples
}

func (w *WindowStats) Average() float64 {
	if w.samples == 0 {
		return 0
	}
	return w.sum / float64(w.samples)
}

func (w *WindowStats) StdDev() float64 {
	if w.samples == 0 {
		return 0
	}
	mean := w.Average()
	variance := w.sumSquares/float64(w.samples) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}

func (w *WindowStats) ZScore(value float64) float64 {
	std := w.StdDev()
	if std == 0 {
		return 0
	}
	return (value - w.Average()) / std
}
