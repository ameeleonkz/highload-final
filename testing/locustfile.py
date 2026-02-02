from locust import HttpUser, task, between, events
import time, random, json, threading

class MetricUser(HttpUser):
    wait_time = between(0.02, 0.05)

    @task
    def send_metric(self):
        ts = int(time.time())
        cpu = max(0.0, min(1.0, random.gauss(0.5, 0.1)))
        rps, is_anomaly = generate_rps()
        payload = {"timestamp": ts, "cpu": cpu, "rps": rps, "device_id": "dev-1"}
        self.client.post("/ingest", json=payload)
        collect_stats(self.client, ts, rps, is_anomaly)


ANOMALY_PROB = 0.05
BASE_RPS = 500.0
BASE_STD = 50.0
ANOMALY_RPS = 900.0
ACCURACY_ERR_THRESHOLD = 0.30

_lock = threading.Lock()
_stats = {
    "total_points": 0,
    "accuracy_hits": 0,
    "normal_points": 0,
    "false_positives": 0,
}


def generate_rps():
    if random.random() < ANOMALY_PROB:
        return ANOMALY_RPS, True
    return max(1.0, random.gauss(BASE_RPS, BASE_STD)), False


def collect_stats(client, ts, actual_rps, is_anomaly):
    try:
        response = client.get("/analytics", name="/analytics", timeout=2)
    except Exception:
        return

    if response.status_code != 200:
        return

    try:
        data = json.loads(response.text)
    except json.JSONDecodeError:
        return

    pred = data.get("rolling_avg_rps")
    anomaly_flag = bool(data.get("anomaly_rps"))

    if pred is None:
        return

    err = abs(pred - actual_rps) / max(actual_rps, 1.0)
    accuracy_hit = err <= ACCURACY_ERR_THRESHOLD

    with _lock:
        _stats["total_points"] += 1
        if accuracy_hit:
            _stats["accuracy_hits"] += 1
        if not is_anomaly:
            _stats["normal_points"] += 1
            if anomaly_flag:
                _stats["false_positives"] += 1


@events.quitting.add_listener
def _(environment, **_kwargs):
    with _lock:
        total_points = _stats["total_points"]
        accuracy_hits = _stats["accuracy_hits"]
        normal_points = _stats["normal_points"]
        false_positives = _stats["false_positives"]

    accuracy = (accuracy_hits / total_points) if total_points else 0.0
    false_positive_rate = (false_positives / normal_points) if normal_points else 0.0

    print("Custom stats:")
    print(f"  total_points={total_points}")
    print(f"  accuracy_hits={accuracy_hits}")
    print(f"  accuracy={accuracy:.2%}")
    print(f"  normal_points={normal_points}")
    print(f"  false_positives={false_positives}")
    print(f"  false_positive_rate={false_positive_rate:.2%}")
