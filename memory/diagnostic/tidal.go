// Package diagnostic holds optional backend inspection and tuning values.
// Conversation and fact contracts do not depend on Tidal kernel details.
package diagnostic

import "time"

type TidalKernelSnapshot struct {
	Key      string
	Feature  string
	BinEdges []time.Duration
	Weights  []float64
	Counts   []int
}

type TidalRerankerConfig struct {
	Beta         float64
	MaxBoost     float64
	LearningRate float64
	MinSamples   int
	Bins         []time.Duration
}

type TidalStoreStats struct {
	QueryEvents    int `json:"query_events"`
	RecallEvents   int `json:"recall_events"`
	FeedbackEvents int `json:"feedback_events"`
	Kernels        int `json:"kernels"`
}
