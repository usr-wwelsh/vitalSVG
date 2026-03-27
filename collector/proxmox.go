package collector

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Proxmox struct {
	host        string
	tokenID     string
	tokenSecret string
	client      *http.Client
}

func NewProxmox(host, tokenID, tokenSecret string, skipTLS bool) *Proxmox {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipTLS,
		},
	}
	return &Proxmox{
		host:        host,
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
	}
}

func (p *Proxmox) Name() string { return "proxmox" }

func (p *Proxmox) Collect() ([]Metric, error) {
	resources, err := p.getResources()
	if err != nil {
		return nil, fmt.Errorf("get resources: %w", err)
	}

	now := time.Now()
	var metrics []Metric

	for _, r := range resources {
		// Only VMs and LXC containers
		if r.Type != "qemu" && r.Type != "lxc" {
			continue
		}

		name := r.Name
		if name == "" {
			name = fmt.Sprintf("%s-%d", r.Type, r.VMID)
		}

		// Status
		statusVal := 0.0
		if r.Status == "running" {
			statusVal = 1.0
		}
		metrics = append(metrics, Metric{Source: "proxmox", Name: name, Kind: "status", Value: statusVal, Time: now})

		// Offline resources get zero metrics
		if r.Status != "running" {
			metrics = append(metrics, Metric{Source: "proxmox", Name: name, Kind: "cpu", Value: 0, Time: now})
			metrics = append(metrics, Metric{Source: "proxmox", Name: name, Kind: "ram", Value: 0, Time: now})
			metrics = append(metrics, Metric{Source: "proxmox", Name: name, Kind: "uptime", Value: 0, Time: now})
			continue
		}

		// CPU percentage
		cpuPct := 0.0
		if r.MaxCPU > 0 {
			cpuPct = (r.CPU / float64(r.MaxCPU)) * 100
		}
		metrics = append(metrics, Metric{Source: "proxmox", Name: name, Kind: "cpu", Value: cpuPct, Time: now})

		// RAM percentage
		ramPct := 0.0
		if r.MaxMem > 0 {
			ramPct = (float64(r.Mem) / float64(r.MaxMem)) * 100
		}
		metrics = append(metrics, Metric{Source: "proxmox", Name: name, Kind: "ram", Value: ramPct, Time: now})

		// Uptime
		if r.Uptime > 0 {
			metrics = append(metrics, Metric{Source: "proxmox", Name: name, Kind: "uptime", Value: float64(r.Uptime), Time: now})
		}
	}

	return metrics, nil
}

// Proxmox API types

type proxmoxResponse struct {
	Data []proxmoxResource `json:"data"`
}

type proxmoxResource struct {
	Type   string  `json:"type"`   // "qemu", "lxc", "storage", "node"
	Name   string  `json:"name"`
	VMID   int     `json:"vmid"`
	Status string  `json:"status"` // "running", "stopped"
	CPU    float64 `json:"cpu"`    // fraction (0.0-1.0 per core)
	MaxCPU int     `json:"maxcpu"`
	Mem    uint64  `json:"mem"`
	MaxMem uint64  `json:"maxmem"`
	Uptime int64   `json:"uptime"` // seconds
}

func (p *Proxmox) getResources() ([]proxmoxResource, error) {
	url := fmt.Sprintf("https://%s/api2/json/cluster/resources?type=vm", p.host)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", p.tokenID, p.tokenSecret))

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxmox API returned %d", resp.StatusCode)
	}

	var result proxmoxResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}
