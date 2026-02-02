# Highload Final

Service for ingesting metrics, storing the latest values in Redis, and exposing Prometheus metrics for monitoring and alerting.

## Quick Start

1. Run Redis locally (default `127.0.0.1:6379`).
2. Start the service:

```bash
go run ./cmd
```

## Configuration

All options are environment variables with defaults:

- `HTTP_ADDR` (default `:8080`)
- `REDIS_ADDR` (default `127.0.0.1:6379`)
- `REDIS_PASSWORD` (default empty)
- `REDIS_DB` (default `0`)
- `ANALYTICS_WINDOW` (default `50`)
- `ANALYTICS_THRESHOLD` (default `2.0`)

## API

### Endpoints

- `POST /ingest` JSON: `{ "timestamp": 1738500000, "cpu": 0.42, "rps": 120.5, "device_id": "dev-1" }`
- `GET /analytics`
- `GET /latest?device_id=dev-1`
- `GET /health`
- `GET /metrics` (Prometheus)

### cURL Examples

```bash
curl -X POST http://localhost:8080/ingest \
  -H "Content-Type: application/json" \
  -d '{ "timestamp": 1738500000, "cpu": 0.42, "rps": 120.5, "device_id": "dev-1" }'
```

```bash
curl http://localhost:8080/analytics
```

```bash
curl "http://localhost:8080/latest?device_id=dev-1"
```

```bash
curl http://localhost:8080/health
```

```bash
curl http://localhost:8080/metrics
```

## Behavior Notes

- Rolling averages and z-scores use a sliding window of 50 samples (by default).
- Redis keeps the latest metric and a list of the most recent 1000 samples.

## Minikube + Ingress + HPA

### 1) Build and load the image

```bash
eval $(minikube -p minikube docker-env)
docker build -t highload:latest .
```

### 2) Install Redis (bitnami/redis)

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update
helm install redis bitnami/redis --set auth.enabled=false
```

The deployment expects Redis at `redis-master:6379`.

### 3) Deploy the app and service

```bash
kubectl apply -f k8s/metrics-app.yaml
```

### 4) Ingress for `/metrics` and `/analytics`

```bash
kubectl apply -f k8s/ingress.yaml
```

Add a hosts entry (example):

```bash
echo "$(minikube ip) metrics.local" | sudo tee -a /etc/hosts
```

Verify:

```bash
curl http://metrics.local/metrics
curl http://metrics.local/analytics
```

### 5) Autoscaling (HPA)

```bash
kubectl apply -f k8s/hpa.yaml
kubectl get hpa
```

## Prometheus + Grafana (Helm)

### 1) Install Prometheus

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install prometheus prometheus-community/prometheus -f k8s/prometheus-values.yaml
```

Prometheus will scrape pods annotated with `prometheus.io/scrape: "true"`.

### 2) Install Grafana

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
helm install grafana grafana/grafana \
  --set sidecar.dashboards.enabled=true \
  --set sidecar.dashboards.label=grafana_dashboard
```

Get the admin password:

```bash
kubectl get secret --namespace default grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

Port-forward Grafana:

```bash
kubectl port-forward svc/grafana 3000:80
```

Apply dashboards:

```bash
kubectl apply -f k8s/grafana-dashboards.yaml
```

### 3) Example metrics

- RPS: `rate(ingest_total[1m])`
- Latency: `ingest_processing_seconds_bucket`
- Anomaly rate (RPS): `rate(analytics_anomaly_rps_total[5m]) / rate(ingest_total[5m])`

### 4) Alertmanager

The alert rule is defined in `k8s/prometheus-values.yaml`:
- `HighAnomalyRate` fires if anomalies exceed 5 per minute for 1 minute.

Port-forward Alertmanager:

```bash
kubectl port-forward svc/prometheus-alertmanager 9093:9093
```
