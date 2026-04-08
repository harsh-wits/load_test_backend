package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

type runInput struct {
	ScenarioID   string `json:"scenario_id"`
	ScenarioName string `json:"scenario_name"`
	Verification bool   `json:"verification_enabled"`
	ErrorInject  bool   `json:"error_injection_enabled"`
	RunID        string `json:"run_id"`
	RPS          int    `json:"rps"`
}

type exportInput struct {
	BaseURL   string     `json:"base_url"`
	SessionID string     `json:"session_id"`
	Runs      []runInput `json:"runs"`
}

type actionMetrics struct {
	Sent    int     `json:"sent"`
	Success int     `json:"success"`
	Failure int     `json:"failure"`
	Timeout int     `json:"timeout"`
	AvgMS   float64 `json:"avg_ms"`
	P90MS   float64 `json:"p90_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
}

type runResponse struct {
	ID             string `json:"id"`
	SessionID      string `json:"session_id"`
	BPPID          string `json:"bpp_id"`
	RPS            int    `json:"rps"`
	DurationSec    int    `json:"duration_sec"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at"`
	CompletedAt    string `json:"completed_at"`
	Metrics        struct {
		Select    actionMetrics `json:"select"`
		OnSelect  actionMetrics `json:"on_select"`
		Init      actionMetrics `json:"init"`
		OnInit    actionMetrics `json:"on_init"`
		Confirm   actionMetrics `json:"confirm"`
		OnConfirm actionMetrics `json:"on_confirm"`
	} `json:"metrics"`
	JourneyMetrics struct {
		Select journeyMetrics `json:"select"`
		Init   journeyMetrics `json:"init"`
		Confirm journeyMetrics `json:"confirm"`
	} `json:"journey_metrics"`
}

type journeyMetrics struct {
	Sent       int     `json:"sent"`
	Received   int     `json:"received"`
	Success    int     `json:"success"`
	Failure    int     `json:"failure"`
	Timeout    int     `json:"timeout"`
	AvgMS      float64 `json:"avg_ms"`
	P90MS      float64 `json:"p90_ms"`
	P95MS      float64 `json:"p95_ms"`
	P99MS      float64 `json:"p99_ms"`
	SuccessPct float64 `json:"success_pct"`
}

type summaryRow struct {
	ScenarioName       string
	Verification       bool
	ErrorInjection     bool
	RunID              string
	RPS                int
	Status             string
	JourneySuccessPct  float64
	SelectSuccessPct   float64
	InitSuccessPct     float64
	ConfirmSuccessPct  float64
	SelectP95MS        float64
	InitP95MS          float64
	ConfirmP95MS       float64
	SelectP99MS        float64
	InitP99MS          float64
	ConfirmP99MS       float64
}

type stageTrendRow struct {
	ScenarioName string
	RPS          int
	Stage        string
	SuccessPct   float64
	P95MS        float64
	P99MS        float64
	Timeout      int
}

type behaviorRow struct {
	ScenarioName        string
	RPS                 int
	ErrorInjection      bool
	ExpectedDropPct     float64
	SelectDropPct       float64
	InitDropPct         float64
	ConfirmDropPct      float64
	SelectCheck         string
	InitCheck           string
	ConfirmCheck        string
	Observation         string
}

func main() {
	inputPath := flag.String("input", "run_export_input.json", "path to export input json")
	outputPath := flag.String("output", "stakeholder_run_report.xlsx", "path to output xlsx")
	timeoutSec := flag.Int("timeout_sec", 30, "http timeout seconds")
	flag.Parse()

	cfg, err := loadInput(*inputPath)
	if err != nil {
		exitErr(err)
	}
	if err := validateInput(cfg); err != nil {
		exitErr(err)
	}

	client := &http.Client{Timeout: time.Duration(*timeoutSec) * time.Second}
	stageRows := make([]stageTrendRow, 0, len(cfg.Runs)*3)
	summaryRows := make([]summaryRow, 0, len(cfg.Runs))
	behaviorRows := make([]behaviorRow, 0, len(cfg.Runs))

	for _, r := range cfg.Runs {
		run, err := fetchRun(client, cfg.BaseURL, cfg.SessionID, r.RunID)
		if err != nil {
			exitErr(fmt.Errorf("fetch run %s failed: %w", r.RunID, err))
		}
		summaryRows = append(summaryRows, toSummaryRow(r, run))
		stageRows = append(stageRows, toStageTrendRows(r, run)...)
		behaviorRows = append(behaviorRows, toBehaviorRow(r, run))
	}

	sort.Slice(summaryRows, func(i, j int) bool {
		if summaryRows[i].ScenarioName != summaryRows[j].ScenarioName {
			return summaryRows[i].ScenarioName < summaryRows[j].ScenarioName
		}
		return summaryRows[i].RPS < summaryRows[j].RPS
	})
	sort.Slice(stageRows, func(i, j int) bool {
		if stageRows[i].ScenarioName != stageRows[j].ScenarioName {
			return stageRows[i].ScenarioName < stageRows[j].ScenarioName
		}
		if stageRows[i].RPS != stageRows[j].RPS {
			return stageRows[i].RPS < stageRows[j].RPS
		}
		return stageRows[i].Stage < stageRows[j].Stage
	})
	sort.Slice(behaviorRows, func(i, j int) bool {
		if behaviorRows[i].ScenarioName != behaviorRows[j].ScenarioName {
			return behaviorRows[i].ScenarioName < behaviorRows[j].ScenarioName
		}
		return behaviorRows[i].RPS < behaviorRows[j].RPS
	})

	if err := writeWorkbook(*outputPath, summaryRows, stageRows, behaviorRows); err != nil {
		exitErr(err)
	}
	fmt.Printf("Excel report written: %s\n", *outputPath)
}

func loadInput(path string) (*exportInput, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg exportInput
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateInput(cfg *exportInput) error {
	if cfg == nil {
		return errors.New("input is nil")
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.SessionID = strings.TrimSpace(cfg.SessionID)
	if cfg.BaseURL == "" || cfg.SessionID == "" {
		return errors.New("base_url and session_id are required")
	}
	if len(cfg.Runs) == 0 {
		return errors.New("runs must not be empty")
	}
	for i := range cfg.Runs {
		cfg.Runs[i].ScenarioID = strings.TrimSpace(cfg.Runs[i].ScenarioID)
		cfg.Runs[i].ScenarioName = strings.TrimSpace(cfg.Runs[i].ScenarioName)
		cfg.Runs[i].RunID = strings.TrimSpace(cfg.Runs[i].RunID)
		if cfg.Runs[i].ScenarioName == "" {
			cfg.Runs[i].ScenarioName = cfg.Runs[i].ScenarioID
		}
		if cfg.Runs[i].ScenarioID == "" {
			cfg.Runs[i].ScenarioID = strings.ToLower(strings.ReplaceAll(cfg.Runs[i].ScenarioName, " ", "_"))
		}
		if cfg.Runs[i].ScenarioName == "" || cfg.Runs[i].RunID == "" || cfg.Runs[i].RPS <= 0 {
			return fmt.Errorf("invalid runs[%d]: scenario_name, run_id, rps are required", i)
		}
	}
	return nil
}

func fetchRun(client *http.Client, baseURL, sessionID, runID string) (*runResponse, error) {
	url := fmt.Sprintf("%s/sessions/%s/runs/%s", baseURL, sessionID, runID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var out runResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func toStageTrendRows(in runInput, run *runResponse) []stageTrendRow {
	return []stageTrendRow{
		{
			ScenarioName: in.ScenarioName, RPS: in.RPS, Stage: "select",
			SuccessPct: run.JourneyMetrics.Select.SuccessPct,
			P95MS:      run.JourneyMetrics.Select.P95MS,
			P99MS:      run.JourneyMetrics.Select.P99MS,
			Timeout:    run.JourneyMetrics.Select.Timeout,
		},
		{
			ScenarioName: in.ScenarioName, RPS: in.RPS, Stage: "init",
			SuccessPct: run.JourneyMetrics.Init.SuccessPct,
			P95MS:      run.JourneyMetrics.Init.P95MS,
			P99MS:      run.JourneyMetrics.Init.P99MS,
			Timeout:    run.JourneyMetrics.Init.Timeout,
		},
		{
			ScenarioName: in.ScenarioName, RPS: in.RPS, Stage: "confirm",
			SuccessPct: run.JourneyMetrics.Confirm.SuccessPct,
			P95MS:      run.JourneyMetrics.Confirm.P95MS,
			P99MS:      run.JourneyMetrics.Confirm.P99MS,
			Timeout:    run.JourneyMetrics.Confirm.Timeout,
		},
	}
}

func toSummaryRow(in runInput, run *runResponse) summaryRow {
	totalSent := run.JourneyMetrics.Select.Sent + run.JourneyMetrics.Init.Sent + run.JourneyMetrics.Confirm.Sent
	totalSuccess := run.JourneyMetrics.Select.Success + run.JourneyMetrics.Init.Success + run.JourneyMetrics.Confirm.Success
	journeyPct := 0.0
	if totalSent > 0 {
		journeyPct = float64(totalSuccess) * 100 / float64(totalSent)
	}
	return summaryRow{
		ScenarioName:      in.ScenarioName,
		Verification:      in.Verification,
		ErrorInjection:    in.ErrorInject,
		RunID:             run.ID,
		RPS:               in.RPS,
		Status:            run.Status,
		JourneySuccessPct: journeyPct,
		SelectSuccessPct:  run.JourneyMetrics.Select.SuccessPct,
		InitSuccessPct:    run.JourneyMetrics.Init.SuccessPct,
		ConfirmSuccessPct: run.JourneyMetrics.Confirm.SuccessPct,
		SelectP95MS:       run.JourneyMetrics.Select.P95MS,
		InitP95MS:         run.JourneyMetrics.Init.P95MS,
		ConfirmP95MS:      run.JourneyMetrics.Confirm.P95MS,
		SelectP99MS:       run.JourneyMetrics.Select.P99MS,
		InitP99MS:         run.JourneyMetrics.Init.P99MS,
		ConfirmP99MS:      run.JourneyMetrics.Confirm.P99MS,
	}
}

func toBehaviorRow(in runInput, run *runResponse) behaviorRow {
	expectedDrop := 0.0
	if in.ErrorInject {
		expectedDrop = 10.0
	}
	selectDrop := 100 - run.JourneyMetrics.Select.SuccessPct
	initDrop := 100 - run.JourneyMetrics.Init.SuccessPct
	confirmDrop := 100 - run.JourneyMetrics.Confirm.SuccessPct

	return behaviorRow{
		ScenarioName:    in.ScenarioName,
		RPS:             in.RPS,
		ErrorInjection:  in.ErrorInject,
		ExpectedDropPct: expectedDrop,
		SelectDropPct:   selectDrop,
		InitDropPct:     initDrop,
		ConfirmDropPct:  confirmDrop,
		SelectCheck:     dropCheck(selectDrop, expectedDrop),
		InitCheck:       dropCheck(initDrop, expectedDrop),
		ConfirmCheck:    dropCheck(confirmDrop, expectedDrop),
		Observation:     buildObservation(in.ErrorInject, selectDrop, initDrop, confirmDrop),
	}
}

func dropCheck(observed, expected float64) string {
	if expected == 0 {
		if observed == 0 {
			return "OK"
		}
		return "Unexpected drop"
	}
	if observed >= expected-2 && observed <= expected+2 {
		return "OK"
	}
	if observed < expected-2 {
		return "Lower than expected"
	}
	return "Higher than expected"
}

func buildObservation(errorInject bool, selectDrop, initDrop, confirmDrop float64) string {
	if !errorInject {
		if selectDrop == 0 && initDrop == 0 && confirmDrop == 0 {
			return "Baseline clean"
		}
		return "Baseline has drops"
	}
	if selectDrop >= 8 && (initDrop < 2 || confirmDrop < 2) {
		return "Select drop visible, init/confirm drop missing (anomaly)"
	}
	if selectDrop >= 8 && initDrop >= 8 && confirmDrop >= 8 {
		return "Drop propagated across stages"
	}
	return "Injection behavior inconsistent"
}

func writeWorkbook(path string, summary []summaryRow, stages []stageTrendRow, behavior []behaviorRow) error {
	f := excelize.NewFile()
	summarySheet := "Executive Summary"
	stageSheet := "Stage Trends"
	behaviorSheet := "Expected vs Observed"
	f.SetSheetName("Sheet1", summarySheet)
	_, _ = f.NewSheet(stageSheet)
	_, _ = f.NewSheet(behaviorSheet)

	summaryHeader := []string{
		"Scenario", "Verification", "Error Injection", "Run ID", "RPS", "Status",
		"Journey Success %",
		"Select Success %", "Init Success %", "Confirm Success %",
		"Select P95 (ms)", "Init P95 (ms)", "Confirm P95 (ms)",
		"Select P99 (ms)", "Init P99 (ms)", "Confirm P99 (ms)",
	}
	stageHeader := []string{
		"Scenario", "RPS", "Stage", "Success %", "P95 (ms)", "P99 (ms)", "Timeout Count",
	}
	behaviorHeader := []string{
		"Scenario", "RPS", "Error Injection", "Expected Drop %", "Select Drop %", "Init Drop %",
		"Confirm Drop %", "Select Check", "Init Check", "Confirm Check", "Observation",
	}

	writeHeader(f, summarySheet, summaryHeader)
	writeHeader(f, stageSheet, stageHeader)
	writeHeader(f, behaviorSheet, behaviorHeader)

	for i, row := range summary {
		rn := i + 2
		values := []any{
			row.ScenarioName, row.Verification, row.ErrorInjection, row.RunID, row.RPS, row.Status, row.JourneySuccessPct,
			row.SelectSuccessPct, row.InitSuccessPct, row.ConfirmSuccessPct,
			row.SelectP95MS, row.InitP95MS, row.ConfirmP95MS,
			row.SelectP99MS, row.InitP99MS, row.ConfirmP99MS,
		}
		writeRow(f, summarySheet, rn, values)
	}
	for i, row := range stages {
		rn := i + 2
		values := []any{
			row.ScenarioName, row.RPS, row.Stage, row.SuccessPct, row.P95MS, row.P99MS, row.Timeout,
		}
		writeRow(f, stageSheet, rn, values)
	}
	for i, row := range behavior {
		rn := i + 2
		values := []any{
			row.ScenarioName, row.RPS, row.ErrorInjection, row.ExpectedDropPct, row.SelectDropPct, row.InitDropPct,
			row.ConfirmDropPct, row.SelectCheck, row.InitCheck, row.ConfirmCheck, row.Observation,
		}
		writeRow(f, behaviorSheet, rn, values)
	}

	for _, sh := range []string{summarySheet, stageSheet, behaviorSheet} {
		if err := f.SetPanes(sh, &excelize.Panes{
			Freeze:      true,
			Split:       false,
			XSplit:      0,
			YSplit:      1,
			TopLeftCell: "A2",
		}); err != nil {
			return err
		}
	}

	_ = f.SetColWidth(summarySheet, "A", "P", 18)
	_ = f.SetColWidth(stageSheet, "A", "G", 18)
	_ = f.SetColWidth(behaviorSheet, "A", "K", 24)

	return f.SaveAs(path)
}

func writeHeader(f *excelize.File, sheet string, headers []string) {
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
}

func writeRow(f *excelize.File, sheet string, row int, values []any) {
	for i, v := range values {
		cell, _ := excelize.CoordinatesToCellName(i+1, row)
		_ = f.SetCellValue(sheet, cell, v)
	}
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
