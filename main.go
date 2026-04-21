package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/wwelsh/vitalsvg/collector"
	"github.com/wwelsh/vitalsvg/server"
	"github.com/wwelsh/vitalsvg/store"
)

const githubRepo = "usr-wwelsh/vitalSVG"

func selfUpdate() {
	type release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	resp, err := http.Get("https://api.github.com/repos/" + githubRepo + "/releases/latest")
	if err != nil {
		fmt.Fprintln(os.Stderr, "update: failed to fetch release:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		fmt.Fprintln(os.Stderr, "update: failed to parse release:", err)
		os.Exit(1)
	}

	assetName := fmt.Sprintf("vitalsvg-%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "update: no asset found for %s\n", assetName)
		os.Exit(1)
	}

	fmt.Printf("updating to %s (%s)...\n", rel.TagName, assetName)

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "update: cannot determine executable path:", err)
		os.Exit(1)
	}
	exe, _ = filepath.EvalSymlinks(exe)

	tmp := exe + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update: cannot write temp file:", err)
		os.Exit(1)
	}

	dlResp, err := http.Get(downloadURL)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		fmt.Fprintln(os.Stderr, "update: download failed:", err)
		os.Exit(1)
	}
	defer dlResp.Body.Close()

	if _, err := io.Copy(f, dlResp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		fmt.Fprintln(os.Stderr, "update: write failed:", err)
		os.Exit(1)
	}
	f.Close()

	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		fmt.Fprintln(os.Stderr, "update: replace failed:", err)
		os.Exit(1)
	}

	fmt.Println("update complete — restart vitalsvg")
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--update" {
		selfUpdate()
		return
	}

	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	st, err := store.New(cfg.DataDir)
	if err != nil {
		slog.Error("failed to open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	// Build list of enabled collectors
	var collectors []collector.Collector

	if cfg.Docker.Enabled {
		dc := collector.NewDocker(cfg.Docker.Socket)
		collectors = append(collectors, dc)
		slog.Info("docker collector enabled", "socket", cfg.Docker.Socket)
	}

	if cfg.Proxmox.Enabled {
		pc := collector.NewProxmox(cfg.Proxmox.Host, cfg.Proxmox.TokenID, cfg.Proxmox.TokenSecret, cfg.Proxmox.SkipTLSVerify)
		collectors = append(collectors, pc)
		slog.Info("proxmox collector enabled", "host", cfg.Proxmox.Host)
	}

	if len(collectors) == 0 {
		slog.Warn("no collectors enabled — badges will show 'unknown' until a collector is configured")
	}

	// Start background polling
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	for _, c := range collectors {
		wg.Add(1)
		go func(c collector.Collector) {
			defer wg.Done()
			runCollector(ctx, c, st, cfg.PollInterval)
		}(c)
	}

	// Start HTTP server
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.New(st),
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)

	wg.Wait()
	slog.Info("goodbye")
}

func runCollector(ctx context.Context, c collector.Collector, st *store.Store, interval time.Duration) {
	// Run immediately on start
	collect(c, st)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect(c, st)
		}
	}
}

func collect(c collector.Collector, st *store.Store) {
	metrics, err := c.Collect()
	if err != nil {
		slog.Error("collection failed", "collector", c.Name(), "err", err)
		return
	}

	var storeMetrics []store.Metric
	for _, m := range metrics {
		storeMetrics = append(storeMetrics, store.Metric{
			Source: m.Source,
			Name:   m.Name,
			Metric: m.Kind,
			Value:  m.Value,
			Ts:     m.Time.Unix(),
		})
	}

	if err := st.InsertBatch(storeMetrics); err != nil {
		slog.Error("store insert failed", "collector", c.Name(), "err", err)
		return
	}

	if err := st.Prune(); err != nil {
		slog.Error("prune failed", "err", err)
	}

	slog.Info("collected", "collector", c.Name(), "metrics", len(metrics))
}
