package model

type Sample struct {
	DeviceID  string  `json:"device_id,omitempty"`
	CPU       float64 `json:"cpu"`
	RPS       float64 `json:"rps"`
	Timestamp int64   `json:"timestamp"`
}
