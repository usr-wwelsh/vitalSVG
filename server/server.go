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
	srv.mux.HandleFunc("GET /api/resources", srv.handleListResources)
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
		badge.RenderMetric(w, label+" cpu", m.Value, "%")

	case "ram":
		// Try to get raw usage bytes for a richer display
		usedM, _ := s.store.QueryLatest(source, name, "ram_used")
		limitM, _ := s.store.QueryLatest(source, name, "ram_limit")
		if usedM != nil && limitM != nil {
			badge.RenderRAM(w, label+" ram", m.Value, usedM.Value, limitM.Value)
		} else {
			badge.RenderMetric(w, label+" ram", m.Value, "%")
		}

	case "uptime":
		badge.RenderMetric(w, label, m.Value, "uptime")
	}
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

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(ui.HTML)
}
