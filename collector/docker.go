package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type Docker struct {
	socket string
	client *http.Client
}

func NewDocker(socket string) *Docker {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socket)
		},
	}
	return &Docker{
		socket: socket,
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
	}
}

func (d *Docker) Name() string { return "docker" }

func (d *Docker) Collect() ([]Metric, error) {
	containers, err := d.listContainers()
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	now := time.Now()
	var metrics []Metric

	for _, c := range containers {
		name := cleanContainerName(c.Names)

		// Status metric
		statusVal := 0.0 // offline
		if c.State == "running" {
			statusVal = 1.0
			// Check health if available
			if c.Status != "" && strings.Contains(strings.ToLower(c.Status), "unhealthy") {
				statusVal = 2.0
			}
		}
		metrics = append(metrics, Metric{Source: "docker", Name: name, Kind: "status", Value: statusVal, Time: now})

		// Only fetch stats for running containers
		if c.State != "running" {
			continue
		}

		stats, err := d.getStats(c.ID)
		if err != nil {
			continue // skip this container's stats, don't fail the whole collection
		}

		// CPU percentage
		cpuPct := calculateCPUPercent(stats)
		metrics = append(metrics, Metric{Source: "docker", Name: name, Kind: "cpu", Value: cpuPct, Time: now})

		// RAM percentage and usage bytes
		ramPct := 0.0
		ramUsed := float64(stats.MemoryStats.Usage)
		if stats.MemoryStats.Stats.Cache > 0 && stats.MemoryStats.Usage > stats.MemoryStats.Stats.Cache {
			ramUsed = float64(stats.MemoryStats.Usage - stats.MemoryStats.Stats.Cache)
		}
		if stats.MemoryStats.Limit > 0 {
			ramPct = (ramUsed / float64(stats.MemoryStats.Limit)) * 100
		}
		metrics = append(metrics, Metric{Source: "docker", Name: name, Kind: "ram", Value: ramPct, Time: now})
		metrics = append(metrics, Metric{Source: "docker", Name: name, Kind: "ram_used", Value: ramUsed, Time: now})
		metrics = append(metrics, Metric{Source: "docker", Name: name, Kind: "ram_limit", Value: float64(stats.MemoryStats.Limit), Time: now})

		// Uptime (seconds since container started)
		if !c.Created.Time.IsZero() {
			uptime := now.Sub(c.Created.Time).Seconds()
			metrics = append(metrics, Metric{Source: "docker", Name: name, Kind: "uptime", Value: uptime, Time: now})
		}
	}

	return metrics, nil
}

// Docker API types

type containerListEntry struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	State   string   `json:"State"`
	Status  string   `json:"Status"`
	Created jsonTime `json:"Created"`
}

type jsonTime struct {
	time.Time
}

func (jt *jsonTime) UnmarshalJSON(b []byte) error {
	var ts int64
	if err := json.Unmarshal(b, &ts); err == nil {
		jt.Time = time.Unix(ts, 0)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return err
		}
		jt.Time = t
		return nil
	}
	return fmt.Errorf("cannot parse time: %s", string(b))
}

type containerStats struct {
	CPUStats    cpuStats    `json:"cpu_stats"`
	PreCPUStats cpuStats    `json:"precpu_stats"`
	MemoryStats memoryStats `json:"memory_stats"`
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage uint64 `json:"total_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     int    `json:"online_cpus"`
}

type memoryStats struct {
	Usage uint64 `json:"usage"`
	Limit uint64 `json:"limit"`
	Stats struct {
		Cache uint64 `json:"cache"`
	} `json:"stats"`
}

func (d *Docker) listContainers() ([]containerListEntry, error) {
	resp, err := d.client.Get("http://localhost/containers/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var containers []containerListEntry
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, err
	}
	return containers, nil
}

func (d *Docker) getStats(containerID string) (*containerStats, error) {
	resp, err := d.client.Get(fmt.Sprintf("http://localhost/containers/%s/stats?stream=false", containerID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var stats containerStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func calculateCPUPercent(stats *containerStats) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemCPUUsage - stats.PreCPUStats.SystemCPUUsage)

	if systemDelta <= 0 || cpuDelta <= 0 {
		return 0
	}

	cpus := stats.CPUStats.OnlineCPUs
	if cpus == 0 {
		cpus = 1
	}

	return (cpuDelta / systemDelta) * float64(cpus) * 100
}

func cleanContainerName(names []string) string {
	if len(names) == 0 {
		return "unknown"
	}
	// Docker prefixes container names with "/"
	return strings.TrimPrefix(names[0], "/")
}
