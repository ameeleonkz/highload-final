package analytics

import (
	"sync"
	"time"

	"highload_final/internal/model"
	"highload_final/internal/windowstats"
)

type Snapshot struct {
	TimeUnix   int64   `json:"timestamp"`
	AvgCPU     float64 `json:"rolling_avg_cpu"`
	AvgRPS     float64 `json:"rolling_avg_rps"`
	ZCPU       float64 `json:"zscore_cpu"`
	ZRPS       float64 `json:"zscore_rps"`
	CPUAnomaly bool    `json:"anomaly_cpu"`
	RPSAnomaly bool    `json:"anomaly_rps"`
	Samples    int     `json:"window_count"`
}

type Analyzer struct {
	cpuWindow *windowstats.WindowStats
	rpsWindow *windowstats.WindowStats
	threshold float64

	mu     sync.RWMutex
	latest Snapshot
}

func NewAnalyzer(window int, threshold float64) *Analyzer {
	return &Analyzer{
		cpuWindow: windowstats.NewWindowStats(window),
		rpsWindow: windowstats.NewWindowStats(window),
		threshold: threshold,
	}
}

func (a *Analyzer) Process(m model.Sample) Snapshot {
	if m.Timestamp == 0 {
		m.Timestamp = time.Now().Unix()
	}

	a.cpuWindow.Push(m.CPU)
	a.rpsWindow.Push(m.RPS)

	zCPU := a.cpuWindow.ZScore(m.CPU)
	zRPS := a.rpsWindow.ZScore(m.RPS)

	res := Snapshot{
		TimeUnix:   m.Timestamp,
		AvgCPU:     a.cpuWindow.Average(),
		AvgRPS:     a.rpsWindow.Average(),
		ZCPU:       zCPU,
		ZRPS:       zRPS,
		CPUAnomaly: absFloat(zCPU) >= a.threshold,
		RPSAnomaly: absFloat(zRPS) >= a.threshold,
		Samples:    a.cpuWindow.Size(),
	}

	a.mu.Lock()
	a.latest = res
	a.mu.Unlock()

	return res
}

func (a *Analyzer) Latest() Snapshot {
	a.mu.RLock()
	res := a.latest
	a.mu.RUnlock()
	return res
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
