package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wwelsh/vitalsvg/badge"
	"github.com/wwelsh/vitalsvg/store"
	"github.com/wwelsh/vitalsvg/ui"
)

type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

func New(s *store.Store) *Server {
	srv := &Server{store: s, mux: http.NewServeMux()}
	srv.mux.HandleFunc("GET /badge/{source}/{name}/{badgeType}", srv.handleBadge)
	srv.mux.HandleFunc("GET /badge/{source}/master.svg", srv.handleMasterBadge)
	srv.mux.HandleFunc("GET /badge/master.svg", srv.handleMasterBadge)
	srv.mux.HandleFunc("GET /api/resources", srv.handleListResources)
	srv.mux.HandleFunc("GET /api/containers", srv.handleAPIContainers)
	srv.mux.HandleFunc("GET /health", srv.handleHealth)
	srv.mux.HandleFunc("GET /", srv.handleUI)
	return srv
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, "ok")
}

func (s *Server) handleBadge(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	name := r.PathValue("name")
	badgeType := strings.TrimSuffix(r.PathValue("badgeType"), ".svg")

	if source != "docker" && source != "proxmox" {
		http.Error(w, "source must be docker or proxmox", http.StatusBadRequest)
		return
	}

	validTypes := map[string]bool{
		"status": true, "cpu": true, "ram": true, "uptime": true, "sparkline": true,
	}
	if !validTypes[badgeType] {
		http.Error(w, "type must be status, cpu, ram, uptime, or sparkline", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=30, s-maxage=30")
	w.Header().Set("Expires", time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat))

	label := name

	if badgeType == "sparkline" {
		// Determine which metric to chart — default to cpu, allow ?metric= override
		metric := r.URL.Query().Get("metric")
		if metric == "" {
			metric = "cpu"
		}
		points, err := s.store.QuerySeries(source, name, metric)
		if err != nil {
			slog.Error("query series", "err", err)
			badge.RenderUnknown(w, label)
			return
		}
		var bp []badge.DataPoint
		for _, p := range points {
			bp = append(bp, badge.DataPoint{Value: p.Value, Time: p.Ts})
		}
		badge.RenderSparkline(w, label+" "+metric, bp)
		return
	}

	// Map badge type to metric name in store
	metric := badgeType

	m, err := s.store.QueryLatest(source, name, metric)
	if err != nil {
		slog.Error("query latest", "err", err)
		badge.RenderUnknown(w, label)
		return
	}
	if m == nil {
		badge.RenderUnknown(w, label)
		return
	}

	// Check if the resource is offline so we can force red on all badges
	isOffline := false
	if badgeType != "status" {
		if sm, _ := s.store.QueryLatest(source, name, "status"); sm != nil && int(sm.Value) == 0 {
			isOffline = true
		}
	}

	switch badgeType {
	case "status":
		status := "unknown"
		switch int(m.Value) {
		case 0:
			status = "offline"
		case 1:
			status = "online"
		case 2:
			status = "degraded"
		}
		badge.RenderStatus(w, label, status)

	case "cpu":
		if isOffline {
			badge.RenderMetricOffline(w, label+" cpu", m.Value, "%")
		} else {
			badge.RenderMetric(w, label+" cpu", m.Value, "%")
		}

	case "ram":
		// Try to get raw usage bytes for a richer display
		usedM, _ := s.store.QueryLatest(source, name, "ram_used")
		limitM, _ := s.store.QueryLatest(source, name, "ram_limit")
		if isOffline {
			if usedM != nil && limitM != nil {
				badge.RenderRAMOffline(w, label+" ram", m.Value, usedM.Value, limitM.Value)
			} else {
				badge.RenderMetricOffline(w, label+" ram", m.Value, "%")
			}
		} else {
			if usedM != nil && limitM != nil {
				badge.RenderRAM(w, label+" ram", m.Value, usedM.Value, limitM.Value)
			} else {
				badge.RenderMetric(w, label+" ram", m.Value, "%")
			}
		}

	case "uptime":
		if isOffline {
			// Show how long the resource has been offline
			downtime := 0.0
			if lastOn, _ := s.store.QueryLastOnline(source, name); lastOn > 0 {
				downtime = float64(time.Now().Unix() - lastOn)
			}
			badge.RenderMetricOffline(w, label+" down", downtime, "uptime")
		} else {
			badge.RenderMetric(w, label, m.Value, "uptime")
		}
	}
}

func (s *Server) handleMasterBadge(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source") // empty string if /badge/master.svg

	resources, err := s.store.ListResources()
	if err != nil {
		slog.Error("list resources for master", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Filter by source if specified
	if source != "" {
		filtered := resources[:0]
		for _, res := range resources {
			if res.Source == source {
				filtered = append(filtered, res)
			}
		}
		resources = filtered
	}

	// Fetch all series data for sparklines
	cpuSeriesAll, _ := s.store.QuerySeriesAll("cpu")
	ramSeriesAll, _ := s.store.QuerySeriesAll("ram")

	var rows []badge.MasterRow
	for i, res := range resources {
		// Latest metrics
		statusVal := 0
		if m, _ := s.store.QueryLatest(res.Source, res.Name, "status"); m != nil {
			statusVal = int(m.Value)
		}
		var cpuPct, ramPct, uptime float64
		if m, _ := s.store.QueryLatest(res.Source, res.Name, "cpu"); m != nil {
			cpuPct = m.Value
		}
		if m, _ := s.store.QueryLatest(res.Source, res.Name, "ram"); m != nil {
			ramPct = m.Value
		}
		if m, _ := s.store.QueryLatest(res.Source, res.Name, "uptime"); m != nil {
			uptime = m.Value
		}

		// For offline resources, show downtime instead of uptime
		if statusVal == 0 {
			if lastOn, _ := s.store.QueryLastOnline(res.Source, res.Name); lastOn > 0 {
				uptime = float64(time.Now().Unix() - lastOn)
			}
		}

		row := badge.NewMasterRow(res.Name, statusVal, uptime, cpuPct, ramPct, nil, nil)

		// Sparkline series
		key := res.Source + "/" + res.Name
		var cpuPoints, ramPoints []badge.DataPoint
		for _, m := range cpuSeriesAll[key] {
			cpuPoints = append(cpuPoints, badge.DataPoint{Value: m.Value, Time: m.Ts})
		}
		for _, m := range ramSeriesAll[key] {
			ramPoints = append(ramPoints, badge.DataPoint{Value: m.Value, Time: m.Ts})
		}
		badge.BuildMasterRowSparklines(&row, i, cpuPoints, ramPoints)

		rows = append(rows, row)
	}

	title := "VitalSVG"
	displaySource := source
	if source == "" {
		displaySource = ""
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=30, s-maxage=30")
	w.Header().Set("Expires", time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat))

	if len(rows) == 0 {
		badge.RenderUnknown(w, "no resources")
		return
	}

	badge.RenderMaster(w, title, displaySource, rows)
}

func (s *Server) handleListResources(w http.ResponseWriter, r *http.Request) {
	resources, err := s.store.ListResources()
	if err != nil {
		slog.Error("list resources", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resources)
}

// ContainerStatus is the JSON response for /api/containers, designed for Waybar scripts.
type ContainerStatus struct {
	Name   string  `json:"name"`
	Online bool    `json:"online"`
	CPU    float64 `json:"cpu"`
	RAM    float64 `json:"ram"`
}

type ContainersResponse struct {
	Containers []ContainerStatus `json:"containers"`
}

func (s *Server) handleAPIContainers(w http.ResponseWriter, r *http.Request) {
	resources, err := s.store.ListResources()
	if err != nil {
		slog.Error("list resources", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	response := ContainersResponse{
		Containers: make([]ContainerStatus, 0, len(resources)),
	}

	for _, res := range resources {
		cs := ContainerStatus{Name: res.Name}

		if m, _ := s.store.QueryLatest(res.Source, res.Name, "status"); m != nil {
			cs.Online = int(m.Value) == 1 || int(m.Value) == 2
		}

		if m, _ := s.store.QueryLatest(res.Source, res.Name, "cpu"); m != nil {
			cs.CPU = m.Value
		}

		if m, _ := s.store.QueryLatest(res.Source, res.Name, "ram"); m != nil {
			cs.RAM = m.Value
		}

		response.Containers = append(response.Containers, cs)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(ui.HTML)
}
