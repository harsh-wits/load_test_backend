package pipeline

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type StageReport struct {
	Stage       string  `json:"stage"`
	Sent        int     `json:"sent"`
	AckCount    int     `json:"ack_count"`
	NackCount   int     `json:"nack_count"`
	NoCallback  int     `json:"no_callback"`
	P50MS       float64 `json:"p50_ms"`
	P95MS       float64 `json:"p95_ms"`
	P99MS       float64 `json:"p99_ms"`
	AvgMS       float64 `json:"avg_ms"`
}

type RunReport struct {
	RunID       string        `json:"run_id"`
	Stages      []StageReport `json:"stages"`
	GeneratedAt time.Time     `json:"generated_at"`
}

type stagePair struct {
	name     string
	sendDir  string
	recvDir  string
}

func GenerateRunReport(runsRoot, runID string) (*RunReport, error) {
	base := filepath.Join(runsRoot, runID, "pipeline_b")
	if _, err := os.Stat(base); err != nil {
		return nil, fmt.Errorf("run directory not found: %w", err)
	}

	pairs := []stagePair{
		{name: "select", sendDir: "select", recvDir: "on_select"},
		{name: "init", sendDir: "init", recvDir: "on_init"},
		{name: "confirm", sendDir: "confirm", recvDir: "on_confirm"},
	}

	var stages []StageReport
	for _, p := range pairs {
		sr := buildStageReport(base, p)
		if sr.Sent > 0 {
			stages = append(stages, sr)
		}
	}

	return &RunReport{
		RunID:       runID,
		Stages:      stages,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func buildStageReport(base string, p stagePair) StageReport {
	sr := StageReport{Stage: p.name}

	sendDir := filepath.Join(base, p.sendDir)
	recvDir := filepath.Join(base, p.recvDir)

	sent := listJSONFiles(sendDir)
	sr.Sent = len(sent)
	if sr.Sent == 0 {
		return sr
	}

	recv := listJSONFiles(recvDir)
	recvSet := map[string]os.FileInfo{}
	for _, fi := range recv {
		recvSet[fi.Name()] = fi
	}

	var latencies []float64

	for _, sfi := range sent {
		rfi, ok := recvSet[sfi.Name()]
		if !ok {
			sr.NoCallback++
			continue
		}

		if isNack(filepath.Join(recvDir, rfi.Name())) {
			sr.NackCount++
			continue
		}

		sr.AckCount++
		lat := rfi.ModTime().Sub(sfi.ModTime())
		if lat >= 0 {
			latencies = append(latencies, float64(lat.Milliseconds()))
		}
	}

	if len(latencies) > 0 {
		sort.Float64s(latencies)
		sr.P50MS = percentile(latencies, 0.50)
		sr.P95MS = percentile(latencies, 0.95)
		sr.P99MS = percentile(latencies, 0.99)
		var sum float64
		for _, v := range latencies {
			sum += v
		}
		sr.AvgMS = math.Round(sum/float64(len(latencies))*100) / 100
	}

	return sr
}

func listJSONFiles(dir string) []os.FileInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []os.FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	return out
}

func isNack(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var env struct {
		Message struct {
			Ack struct {
				Status string `json:"status"`
			} `json:"ack"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return false
	}
	return env.Message.Ack.Status != "" && env.Message.Ack.Status != "ACK"
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func WriteRunReport(runsRoot string, report *RunReport) (string, error) {
	dir := filepath.Join(runsRoot, report.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}
	path := filepath.Join(dir, "report.json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	return path, nil
}
