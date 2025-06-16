package maxcpu

type GetHelloResponse struct {
	Message string `json:"message"`
}

type GetStatsResponse struct {
	Error   string    `json:"error"`
	Metrics []*Metric `json:"metrics"`
}

type Metric struct {
	Key    string  `json:"key"`
	Metric float64 `json:"metric"`
	Epoch  int64   `json:"epoch"`
}
