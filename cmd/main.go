package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"highload_final/internal/analytics"
	"highload_final/internal/model"
	"highload_final/internal/persistence"
)

const (
	defaultHTTPAddr   = ":8080"
	defaultRedisAddr  = "127.0.0.1:6379"
	defaultRedisDB    = 0
	defaultWindowSize = 50
	defaultThreshold  = 2.0
)

type app struct {
	store      *persistence.MetricStore
	analyzer   *analytics.Analyzer
	queue      chan model.Sample
	ctx        context.Context
	cancel     context.CancelFunc
	windowSize int
	threshold  float64
	prom       promMetrics
}

type promMetrics struct {
	ingestedTotal   prometheus.Counter
	badReqTotal     prometheus.Counter
	queueFullTotal  prometheus.Counter
	redisErrTotal   prometheus.Counter
	procTimeSeconds prometheus.Histogram
	avgCPU          prometheus.Gauge
	avgRPS          prometheus.Gauge
	zCPU            prometheus.Gauge
	zRPS            prometheus.Gauge
	anomCPU         prometheus.Gauge
	anomRPS         prometheus.Gauge
	windowCount     prometheus.Gauge
	anomCPUTotal    prometheus.Counter
	anomRPSTotal    prometheus.Counter
}

func main() {
	cfg := readConfig()

	store := persistence.NewMetricStore(cfg.redisAddr, cfg.redisPassword, cfg.redisDB)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Check(ctx); err != nil {
		log.Printf("redis ping failed: %v", err)
	}

	engine := analytics.NewAnalyzer(cfg.windowSize, cfg.threshold)

	service := newApp(ctx, store, engine, cfg.windowSize, cfg.threshold)
	go service.workerLoop()

	httpServer := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           service.router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("http server listening on %s", cfg.httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen failed: %v", err)
		}
	}()

	awaitSignal(cancel)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	if err := store.Stop(); err != nil {
		log.Printf("redis close error: %v", err)
	}
}

type config struct {
	httpAddr      string
	redisAddr     string
	redisPassword string
	redisDB       int
	windowSize    int
	threshold     float64
}

func readConfig() config {
	return config{
		httpAddr:      readEnv("HTTP_ADDR", defaultHTTPAddr),
		redisAddr:     readEnv("REDIS_ADDR", defaultRedisAddr),
		redisPassword: readEnv("REDIS_PASSWORD", ""),
		redisDB:       readEnvInt("REDIS_DB", defaultRedisDB),
		windowSize:    readEnvInt("ANALYTICS_WINDOW", defaultWindowSize),
		threshold:     readEnvFloat("ANALYTICS_THRESHOLD", defaultThreshold),
	}
}

func readEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func readEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func readEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func newApp(ctx context.Context, store *persistence.MetricStore, engine *analytics.Analyzer, windowSize int, threshold float64) *app {
	queue := make(chan model.Sample, 1024)

	service := &app{
		store:      store,
		analyzer:   engine,
		queue:      queue,
		windowSize: windowSize,
		threshold:  threshold,
		prom:       buildPromMetrics(),
	}
	service.ctx, service.cancel = context.WithCancel(ctx)
	service.prom.register()

	return service
}

func buildPromMetrics() promMetrics {
	return promMetrics{
		ingestedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ingest_total",
			Help: "Total metrics ingested",
		}),
		badReqTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ingest_bad_request_total",
			Help: "Total bad ingest requests",
		}),
		queueFullTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ingest_queue_full_total",
			Help: "Total ingest requests rejected because queue is full",
		}),
		redisErrTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "redis_error_total",
			Help: "Total redis errors",
		}),
		procTimeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ingest_processing_seconds",
			Help:    "Latency for processing metrics",
			Buckets: prometheus.DefBuckets,
		}),
		avgCPU: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "analytics_rolling_avg_cpu",
			Help: "Rolling average of CPU",
		}),
		avgRPS: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "analytics_rolling_avg_rps",
			Help: "Rolling average of RPS",
		}),
		zCPU: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "analytics_zscore_cpu",
			Help: "Z-score for CPU",
		}),
		zRPS: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "analytics_zscore_rps",
			Help: "Z-score for RPS",
		}),
		anomCPU: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "analytics_anomaly_cpu",
			Help: "CPU anomaly flag (1 if anomaly)",
		}),
		anomRPS: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "analytics_anomaly_rps",
			Help: "RPS anomaly flag (1 if anomaly)",
		}),
		windowCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "analytics_window_count",
			Help: "Number of samples in analytics window",
		}),
		anomCPUTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "analytics_anomaly_cpu_total",
			Help: "Total CPU anomalies detected",
		}),
		anomRPSTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "analytics_anomaly_rps_total",
			Help: "Total RPS anomalies detected",
		}),
	}
}

func (m promMetrics) register() {
	prometheus.MustRegister(
		m.ingestedTotal,
		m.badReqTotal,
		m.queueFullTotal,
		m.redisErrTotal,
		m.procTimeSeconds,
		m.avgCPU,
		m.avgRPS,
		m.zCPU,
		m.zRPS,
		m.anomCPU,
		m.anomRPS,
		m.windowCount,
		m.anomCPUTotal,
		m.anomRPSTotal,
	)
}

func (a *app) router() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", a.healthHandler)
	mux.HandleFunc("/ingest", a.ingestHandler)
	mux.HandleFunc("/analytics", a.analyticsHandler)
	mux.HandleFunc("/latest", a.latestHandler)
	return mux
}

func (a *app) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := a.store.Check(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("redis unavailable"))
		return
	}

	_, _ = w.Write([]byte("ok"))
}

func (a *app) ingestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()
	defer func() {
		a.prom.procTimeSeconds.Observe(time.Since(start).Seconds())
	}()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var sample model.Sample
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&sample); err != nil {
		a.prom.badReqTotal.Inc()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid json"))
		return
	}

	if !isSampleValid(sample) {
		a.prom.badReqTotal.Inc()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid metric values"))
		return
	}

	if sample.Timestamp == 0 {
		sample.Timestamp = time.Now().Unix()
	}

	select {
	case a.queue <- sample:
		a.prom.ingestedTotal.Inc()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	default:
		a.prom.queueFullTotal.Inc()
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("queue full"))
	}
}

func isSampleValid(m model.Sample) bool {
	if math.IsNaN(m.CPU) || math.IsNaN(m.RPS) {
		return false
	}
	if math.IsInf(m.CPU, 0) || math.IsInf(m.RPS, 0) {
		return false
	}
	return true
}

func (a *app) analyticsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	res := a.analyzer.Latest()
	respondJSON(w, res)
}

func (a *app) latestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	deviceID := r.URL.Query().Get("device_id")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	metric, err := a.store.FetchLatest(ctx, deviceID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("redis error"))
		return
	}
	if metric == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no data"))
		return
	}

	respondJSON(w, metric)
}

func (a *app) workerLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case sample := <-a.queue:
			ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
			if err := a.store.Save(ctx, sample); err != nil {
				a.prom.redisErrTotal.Inc()
				log.Printf("redis store error: %v", err)
			}
			cancel()

			result := a.analyzer.Process(sample)
			a.updateAnalyticsGauges(result)
		}
	}
}

func (a *app) updateAnalyticsGauges(res analytics.Snapshot) {
	a.prom.avgCPU.Set(res.AvgCPU)
	a.prom.avgRPS.Set(res.AvgRPS)
	a.prom.zCPU.Set(res.ZCPU)
	a.prom.zRPS.Set(res.ZRPS)
	if res.CPUAnomaly {
		a.prom.anomCPU.Set(1)
		a.prom.anomCPUTotal.Inc()
	} else {
		a.prom.anomCPU.Set(0)
	}
	if res.RPSAnomaly {
		a.prom.anomRPS.Set(1)
		a.prom.anomRPSTotal.Inc()
	} else {
		a.prom.anomRPS.Set(0)
	}
	a.prom.windowCount.Set(float64(res.Samples))
}

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func awaitSignal(cancel context.CancelFunc) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()
	log.Printf("shutdown signal received")
}
