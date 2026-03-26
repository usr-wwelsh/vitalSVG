package collector

import "time"

// Metric represents a single data point from any source.
type Metric struct {
	Source string
	Name   string
	Kind   string // "status", "cpu", "ram", "uptime"
	Value  float64
	Time   time.Time
}

// Collector polls a data source and returns current metrics.
type Collector interface {
	Name() string
	Collect() ([]Metric, error)
}
