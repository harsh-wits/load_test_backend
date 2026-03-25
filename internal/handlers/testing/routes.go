package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"seller_app_load_tester/internal/config"
	"seller_app_load_tester/internal/domain/latency"
	domainPipeline "seller_app_load_tester/internal/domain/pipeline"
	"seller_app_load_tester/internal/domain/session"
	"seller_app_load_tester/internal/ports/seller"
	"seller_app_load_tester/internal/shared/apierror"
	"seller_app_load_tester/internal/shared/runlog"
)

type Controller struct {
	cfg         *config.Config
	sessions    *session.Manager
	coordinator *domainPipeline.BCoordinator
	seller      seller.Client
	notifier    *domainPipeline.CallbackNotifier
	store       runlog.Store
	rateLimiter domainPipeline.Throttle
}

func NewController(
	cfg *config.Config,
	sessions *session.Manager,
	coordinator *domainPipeline.BCoordinator,
	sellerClient seller.Client,
	notifier *domainPipeline.CallbackNotifier,
	store runlog.Store,
	rateLimiter domainPipeline.Throttle,
) *Controller {
	return &Controller{
		cfg:         cfg,
		sessions:    sessions,
		coordinator: coordinator,
		seller:      sellerClient,
		notifier:    notifier,
		store:       store,
		rateLimiter: rateLimiter,
	}
}

func (c *Controller) Register(app *fiber.App) {
	s := app.Group("/sessions")
	s.Get("/", c.listSessions)
	s.Post("/", c.createSession)
	s.Delete("/", c.clearSessions)
	s.Get("/:id", c.getSession)
	s.Delete("/:id", c.deleteSession)
	s.Get("/:id/discovery/payload", c.generateSearchPayload)
	s.Post("/:id/discovery", c.startDiscovery)
	s.Put("/:id/catalog", c.uploadCatalog)
	s.Post("/:id/preorder", c.startPreorder)
	s.Get("/:id/runs", c.listRuns)
	s.Get("/:id/runs/:run_id", c.getRun)
	s.Post("/:id/runs/:run_id/stop", c.stopRun)
	s.Get("/:id/report", c.sessionReport)
}

// --- Session CRUD ---

func (c *Controller) listSessions(ctx *fiber.Ctx) error {
	bppID := ctx.Query("bpp_id")
	if bppID == "" {
		return apierror.NewCustomError(400, "REQUIRED_FIELDS_4001", "bpp_id query parameter is required")
	}
	page := ctx.QueryInt("page", 1)
	pageSize := ctx.QueryInt("page_size", 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	sessions, total, err := c.sessions.GetSessionsByBPP(ctx.Context(), bppID, page, pageSize)
	if err != nil {
		return err
	}
	if sessions == nil {
		sessions = []*session.Session{}
	}
	return ctx.JSON(fiber.Map{
		"sessions":  sessions,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	})
}

type createSessionRequest struct {
	BPPID       string  `json:"bpp_id"`
	BPPURI      string  `json:"bpp_uri"`
	CoreVersion *string `json:"core_version"`
	Domain      *string `json:"domain"`
}

func (c *Controller) createSession(ctx *fiber.Ctx) error {
	var req createSessionRequest
	if err := ctx.BodyParser(&req); err != nil {
		return apierror.ErrInvalidRequestBody
	}
	if req.BPPID == "" || req.BPPURI == "" {
		return apierror.NewCustomError(400, "REQUIRED_FIELDS_4001", "bpp_id and bpp_uri are required")
	}

	coreVersion := ""
	domain := ""
	if req.CoreVersion != nil {
		coreVersion = strings.TrimSpace(*req.CoreVersion)
		if coreVersion != "" && !session.IsValidCoreVersion(coreVersion) {
			return apierror.NewCustomError(400, "INVALID_CORE_VERSION", "core_version must be either 1.2.0 or 1.2.5")
		}
	}
	if coreVersion == "" {
		coreVersion = strings.TrimSpace(c.cfg.CoreVersion)
		if !session.IsValidCoreVersion(coreVersion) {
			return apierror.NewCustomError(500, "INVALID_CORE_VERSION_CONFIG", "CORE_VERSION in configuration is not supported")
		}
	}

	if req.Domain != nil {
		domain = strings.TrimSpace(*req.Domain)
		if domain != "" && !session.IsValidDomain(domain) {
			return apierror.NewCustomError(400, "INVALID_DOMAIN", "domain must be one of ONDC:RET10-ONDC:RET16 or ONDC:RET18")
		}
	}
	if domain == "" {
		domain = strings.TrimSpace(c.cfg.Domain)
		if !session.IsValidDomain(domain) {
			return apierror.NewCustomError(500, "INVALID_DOMAIN_CONFIG", "DOMAIN in configuration is not supported")
		}
	}

	sess, err := c.sessions.Create(ctx.Context(), req.BPPID, req.BPPURI, coreVersion, domain)
	if err != nil {
		return err
	}
	return ctx.Status(fiber.StatusCreated).JSON(sess)
}

func (c *Controller) getSession(ctx *fiber.Ctx) error {
	sess, err := c.sessions.GetAny(ctx.Context(), ctx.Params("id"))
	if err != nil {
		return err
	}
	catalog, _ := c.sessions.GetCatalogState(ctx.Context(), sess.ID)
	preorder, _ := c.sessions.GetPreorderState(ctx.Context(), sess.ID)

	return ctx.JSON(fiber.Map{
		"session":  sess,
		"catalog":  catalog,
		"preorder": preorder,
	})
}

func (c *Controller) deleteSession(ctx *fiber.Ctx) error {
	if err := c.sessions.Delete(ctx.Context(), ctx.Params("id")); err != nil {
		return err
	}
	return ctx.SendStatus(fiber.StatusNoContent)
}

func (c *Controller) clearSessions(ctx *fiber.Ctx) error {
	bppID := ctx.Query("bpp_id")
	if bppID == "" {
		return apierror.NewCustomError(400, "REQUIRED_FIELDS_4001", "bpp_id query parameter is required")
	}
	deleted, err := c.sessions.DeleteAllByBPP(ctx.Context(), bppID)
	if err != nil {
		return err
	}
	return ctx.JSON(fiber.Map{"deleted": deleted})
}

// --- Discovery ---

func (c *Controller) generateSearchPayload(ctx *fiber.Ctx) error {
	sess, err := c.sessions.Get(ctx.Context(), ctx.Params("id"))
	if err != nil {
		return err
	}

	raw, err := c.loadSearchPayload(sess)
	if err != nil {
		return apierror.NewCustomError(500, "PIPELINE_5003", err.Error())
	}

	// Patch context so the preview mirrors what will actually be sent:
	// fresh transaction_id, message_id, timestamp, and ttl.
	patched, err := c.patchSearchContext(raw, uuid.NewString())
	if err != nil {
		return apierror.ErrInvalidRequestBody
	}

	var payload any
	if err := json.Unmarshal(patched, &payload); err != nil {
		return apierror.NewCustomError(500, "PIPELINE_5003", "search template is not valid JSON")
	}

	return ctx.JSON(fiber.Map{
		"payload": payload,
		"bpp_uri": sess.BPPURI,
	})
}

type discoveryRequest struct {
	Payload json.RawMessage `json:"payload"`
}

func (c *Controller) startDiscovery(ctx *fiber.Ctx) error {
	sess, err := c.sessions.Get(ctx.Context(), ctx.Params("id"))
	if err != nil {
		return err
	}

	cs, _ := c.sessions.GetCatalogState(ctx.Context(), sess.ID)
	if cs != nil && cs.Status == session.CatalogPending {
		return apierror.ErrCatalogPending
	}

	var searchPayload []byte

	var req discoveryRequest
	if err := ctx.BodyParser(&req); err == nil && len(req.Payload) > 0 {
		searchPayload = req.Payload
	} else {
		searchPayload, err = c.loadSearchPayload(sess)
		if err != nil {
			return apierror.NewCustomError(500, "PIPELINE_5003", err.Error())
		}
	}

	searchTxnID := uuid.NewString()
	searchPayload, err = c.patchSearchContext(searchPayload, searchTxnID)
	if err != nil {
		return apierror.ErrInvalidRequestBody
	}

	log.Printf("[discovery] session=%s search_txn_id=%s", sess.ID, searchTxnID)

	respBody, err := c.seller.Search(ctx.Context(), sess.BPPURI, searchPayload)
	if err != nil {
		_ = c.sessions.SetCatalogState(ctx.Context(), sess.ID, &session.CatalogState{
			Status: session.CatalogFailed, UpdatedAt: time.Now().UTC(),
		})
		return apierror.NewCustomError(502, "PIPELINE_5002", err.Error())
	}

	if isNack(respBody) {
		_ = c.sessions.SetCatalogState(ctx.Context(), sess.ID, &session.CatalogState{
			Status: session.CatalogFailed, UpdatedAt: time.Now().UTC(),
		})
		var details any
		_ = json.Unmarshal(respBody, &details)
		return apierror.NewCustomError(502, "PIPELINE_5002", "BPP returned NACK", details)
	}

	_ = c.sessions.SetCatalogState(ctx.Context(), sess.ID, &session.CatalogState{
		Status: session.CatalogPending, UpdatedAt: time.Now().UTC(),
	})

	ttl := time.Duration(c.cfg.DiscoveryWaitTTLSeconds) * time.Second
	onSearchPayload := c.pollDiscoveryPayload(ctx.Context(), searchTxnID, ttl)
	if onSearchPayload == nil {
		log.Printf("[discovery] on_search timeout session=%s", sess.ID)
		_ = c.sessions.SetCatalogState(ctx.Context(), sess.ID, &session.CatalogState{
			Status: session.CatalogFailed, UpdatedAt: time.Now().UTC(),
		})
		return apierror.ErrUpstreamTimeout
	}

	_ = c.sessions.Persist().SaveCatalog(ctx.Context(), sess.ID, onSearchPayload)
	_ = c.sessions.SetCatalogState(ctx.Context(), sess.ID, &session.CatalogState{
		Status:     session.CatalogReady,
		OnSearchID: searchTxnID,
		UpdatedAt:  time.Now().UTC(),
	})
	log.Printf("[discovery] complete session=%s on_search_id=%s", sess.ID, searchTxnID)

	var onSearch any
	_ = json.Unmarshal(onSearchPayload, &onSearch)

	return ctx.JSON(fiber.Map{
		"session_id":     sess.ID,
		"search_ack":     true,
		"on_search":      onSearch,
		"catalog_status": "ready",
	})
}

// --- Manual Catalog Upload ---

type uploadCatalogRequest struct {
	OnSearch json.RawMessage `json:"on_search"`
}

func (c *Controller) uploadCatalog(ctx *fiber.Ctx) error {
	sess, err := c.sessions.Get(ctx.Context(), ctx.Params("id"))
	if err != nil {
		return err
	}

	var req uploadCatalogRequest
	if err := ctx.BodyParser(&req); err != nil || len(req.OnSearch) == 0 {
		return apierror.ErrInvalidRequestBody
	}

	if !json.Valid(req.OnSearch) {
		return apierror.ErrInvalidRequestBody
	}

	_ = c.sessions.Persist().SaveCatalog(ctx.Context(), sess.ID, req.OnSearch)
	_ = c.sessions.SetCatalogState(ctx.Context(), sess.ID, &session.CatalogState{
		Status:    session.CatalogReady,
		UpdatedAt: time.Now().UTC(),
	})

	return ctx.JSON(fiber.Map{
		"session_id":     sess.ID,
		"catalog_status": "ready",
	})
}

// --- Preorder ---

type preorderRequest struct {
	RPS           int    `json:"rps"`
	DurationSec   int    `json:"duration_sec"`
	TransactionID string `json:"transaction_id,omitempty"`
}

func (c *Controller) startPreorder(ctx *fiber.Ctx) error {
	sess, err := c.sessions.Get(ctx.Context(), ctx.Params("id"))
	if err != nil {
		return err
	}

	cs, _ := c.sessions.GetCatalogState(ctx.Context(), sess.ID)
	if cs == nil || cs.Status != session.CatalogReady {
		return apierror.ErrCatalogNotReady
	}

	var req preorderRequest
	if err := ctx.BodyParser(&req); err != nil {
		return apierror.ErrInvalidRequestBody
	}
	if req.RPS <= 0 {
		req.RPS = c.cfg.DefaultRPS
	}
	if req.DurationSec <= 0 {
		req.DurationSec = int(c.cfg.DefaultDuration.Seconds())
	}

	run, err := c.sessions.StartRun(ctx.Context(), sess.ID, sess.BPPID, req.RPS, req.DurationSec)
	if err != nil {
		return err
	}

	catalogPayload, err := c.sessions.Persist().GetCatalog(ctx.Context(), sess.ID)
	if err != nil {
		_ = c.sessions.FinishRun(ctx.Context(), sess.ID, run.ID, "failed")
		return apierror.ErrCatalogNotReady
	}

	batchSize := req.RPS * req.DurationSec
	var selects []domainPipeline.SelectPayload
	if req.TransactionID != "" {
		selects, err = c.coordinator.SelectBatchFromOnSearchWithTxnID(domainPipeline.OnSearchPayload(catalogPayload), batchSize, req.TransactionID)
	} else {
		selects, err = c.coordinator.SelectBatchFromOnSearch(domainPipeline.OnSearchPayload(catalogPayload), batchSize)
	}
	if err != nil {
		_ = c.sessions.FinishRun(ctx.Context(), sess.ID, run.ID, "failed")
		return apierror.NewCustomError(500, "PIPELINE_5003", err.Error())
	}

	txnIDs := domainPipeline.ExtractTransactionIDs(selects)
	maxInFlight := c.cfg.MaxInFlight
	baseURL := sess.BPPURI

	c.coordinator.SetTxnLinker(func(runID, txnID string) {
		_ = c.sessions.LinkTxn(context.Background(), txnID, runID, sess.ID)
	})
	c.coordinator.SetThrottle(c.rateLimiter, sess.ID)
	c.coordinator.SetCoreVersion(sess.CoreVersion)

	log.Printf("[preorder] starting session=%s run=%s batch=%d max_in_flight=%d",
		sess.ID, run.ID, batchSize, maxInFlight)

	go func() {
		bgCtx := context.Background()
		defer func() {
			status := "completed"
			if bgCtx.Err() != nil {
				status = "failed"
			}
			_ = c.finalizeRunLatencies(bgCtx, sess.ID, run.ID)
			_ = c.sessions.FinishRun(bgCtx, sess.ID, run.ID, status)
			c.notifier.Reset(run.ID)
			c.flushRun(run.ID)
		}()

		c.runPreorder(bgCtx, run.ID, sess.ID, baseURL, selects, txnIDs, maxInFlight)
	}()

	return ctx.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"run_id":     run.ID,
		"session_id": sess.ID,
	})
}

// --- Run listing, polling, and stop ---

func (c *Controller) listRuns(ctx *fiber.Ctx) error {
	sessionID := ctx.Params("id")
	if _, err := c.sessions.GetAny(ctx.Context(), sessionID); err != nil {
		return err
	}
	runs, err := c.sessions.GetRunHistory(ctx.Context(), sessionID)
	if err != nil {
		return err
	}
	if runs == nil {
		runs = []*session.Run{}
	}
	return ctx.JSON(runs)
}

func (c *Controller) getRun(ctx *fiber.Ctx) error {
	run, err := c.sessions.GetRun(ctx.Context(), ctx.Params("run_id"))
	if err != nil {
		return apierror.ErrRunNotFound
	}
	return ctx.JSON(run)
}

func (c *Controller) stopRun(ctx *fiber.Ctx) error {
	sessionID := ctx.Params("id")
	runID, err := c.sessions.StopRun(ctx.Context(), sessionID)
	if err != nil {
		return err
	}
	log.Printf("[preorder] stop requested session=%s run=%s", sessionID, runID)
	return ctx.JSON(fiber.Map{"stopped_run_id": runID})
}

// --- Report ---

func (c *Controller) sessionReport(ctx *fiber.Ctx) error {
	sessionID := ctx.Params("id")
	if _, err := c.sessions.GetAny(ctx.Context(), sessionID); err != nil {
		return err
	}
	runs, err := c.sessions.GetRunHistory(ctx.Context(), sessionID)
	if err != nil {
		return err
	}

	type row struct {
		sent, success, failure, timeout int64
	}
	stages := []string{"select", "on_select", "init", "on_init", "confirm", "on_confirm"}
	agg := make(map[string]*row, len(stages))
	for _, s := range stages {
		agg[s] = &row{}
	}

	for _, r := range runs {
		for _, s := range stages {
			var m session.ActionMetrics
			switch s {
			case "select":
				m = r.Metrics.Select
			case "on_select":
				m = r.Metrics.OnSelect
			case "init":
				m = r.Metrics.Init
			case "on_init":
				m = r.Metrics.OnInit
			case "confirm":
				m = r.Metrics.Confirm
			case "on_confirm":
				m = r.Metrics.OnConfirm
			}
			a := agg[s]
			a.sent += m.Sent
			a.success += m.Success
			a.failure += m.Failure
			a.timeout += m.Timeout
		}
	}

	var b strings.Builder
	b.WriteString("stage,total_sent,total_success,total_failure,total_timeout,run_count\n")
	for _, s := range stages {
		a := agg[s]
		b.WriteString(fmt.Sprintf("%s,%d,%d,%d,%d,%d\n", s, a.sent, a.success, a.failure, a.timeout, len(runs)))
	}

	ctx.Set("Content-Type", "text/csv")
	ctx.Set("Content-Disposition", fmt.Sprintf("attachment; filename=report_%s.csv", sessionID))
	return ctx.SendString(b.String())
}

// --- Preorder stage execution ---

func (c *Controller) runPreorder(
	ctx context.Context, runID, sessionID, baseURL string,
	selects []domainPipeline.SelectPayload,
	txnIDs []string, maxInFlight int,
) {
	coordStore := c.coordinator.Store()
	gap := time.Duration(c.cfg.PipelineStageGapSeconds) * time.Second

	results := c.coordinator.RunSelectStage(ctx, runID, baseURL, selects, maxInFlight)
	selectSent := countSuccess(results)
	c.updateMetrics(ctx, runID, "select", results)
	log.Printf("[preorder] select done run=%s sent=%d errors=%d", runID, selectSent, countErrors(results))

	if selectSent == 0 || ctx.Err() != nil {
		return
	}

	onSelCount := c.notifier.WaitForCount(runID, "on_select", selectSent, 30*time.Second)
	log.Printf("[preorder] on_select run=%s expected=%d received=%d", runID, selectSent, onSelCount)

	if gap > 0 {
		time.Sleep(gap)
	}
	if ctx.Err() != nil {
		return
	}

	var inits []domainPipeline.InitPayload
	var err error
	inits, err = domainPipeline.BuildInitBatchFromOnSelect(coordStore, runID, txnIDs)
	if err != nil {
		initPath := filepath.Join("examples", "payloads", "init", "init.json")
		inits, err = domainPipeline.BuildInitBatchFromExample(initPath, txnIDs)
	}
	if err != nil {
		log.Printf("[preorder] init batch build failed run=%s error=%v", runID, err)
		return
	}

	initResults := c.coordinator.RunInitStage(ctx, runID, baseURL, inits, maxInFlight)
	initSent := countSuccess(initResults)
	c.updateMetrics(ctx, runID, "init", initResults)
	log.Printf("[preorder] init done run=%s sent=%d errors=%d", runID, initSent, countErrors(initResults))

	if initSent == 0 || ctx.Err() != nil {
		return
	}

	onInitCount := c.notifier.WaitForCount(runID, "on_init", initSent, 30*time.Second)
	log.Printf("[preorder] on_init run=%s expected=%d received=%d", runID, initSent, onInitCount)

	if gap > 0 {
		time.Sleep(gap)
	}
	if ctx.Err() != nil {
		return
	}

	var confirms []domainPipeline.ConfirmPayload
	confirms, err = domainPipeline.BuildConfirmBatchFromOnInit(coordStore, runID, txnIDs)
	if err != nil {
		confirmPath := filepath.Join("examples", "payloads", "confirm", "confirm.json")
		confirms, err = domainPipeline.BuildConfirmBatchFromExample(confirmPath, txnIDs)
	}
	if err != nil {
		log.Printf("[preorder] confirm batch build failed run=%s error=%v", runID, err)
		return
	}

	confirmResults := c.coordinator.RunConfirmStage(ctx, runID, baseURL, confirms, maxInFlight)
	confirmSent := countSuccess(confirmResults)
	c.updateMetrics(ctx, runID, "confirm", confirmResults)
	log.Printf("[preorder] confirm done run=%s sent=%d errors=%d", runID, confirmSent, countErrors(confirmResults))

	if confirmSent > 0 {
		onConfirmCount := c.notifier.WaitForCount(runID, "on_confirm", confirmSent, 30*time.Second)
		log.Printf("[preorder] on_confirm run=%s expected=%d received=%d", runID, confirmSent, onConfirmCount)
	}
}

func (c *Controller) finalizeRunLatencies(ctx context.Context, sessionID, runID string) error {
	if c.store == nil || c.sessions == nil {
		return nil
	}
	persist := c.sessions.Persist()
	if persist == nil {
		return nil
	}

	const callbackTimeout = 30 * time.Second
	cutoffAt := time.Now().UTC()
	timeoutThresholdMS := callbackTimeout.Milliseconds()

	type stagePair struct {
		reqAction string
		cbStage   latency.Stage
	}

	pairs := []stagePair{
		{reqAction: "select", cbStage: latency.StageOnSelect},
		{reqAction: "init", cbStage: latency.StageOnInit},
		{reqAction: "confirm", cbStage: latency.StageOnConfirm},
	}

	for _, p := range pairs {
		txnIDs, err := c.store.ListTxnIDs(runID, "pipeline_b", p.reqAction)
		if err != nil {
			log.Printf("[latency] list sent txn_ids FAILED run=%s action=%s error=%v", runID, p.reqAction, err)
			continue
		}

		existing, err := persist.GetRunLatencyEvents(ctx, runID, p.cbStage, txnIDs)
		if err != nil {
			log.Printf("[latency] fetch existing events FAILED run=%s stage=%s error=%v", runID, p.cbStage, err)
			continue
		}

		total := int64(len(txnIDs))
		var successCount, failureCount, timeoutCount int64
		successLatenciesMs := make([]int64, 0, len(txnIDs))

		for _, txnID := range txnIDs {
			if ev, ok := existing[txnID]; ok && ev != nil {
				switch ev.Outcome {
				case latency.OutcomeSuccess:
					successCount++
					if ev.LatencyMS != nil {
						successLatenciesMs = append(successLatenciesMs, *ev.LatencyMS)
					}
				case latency.OutcomeFailure:
					failureCount++
				case latency.OutcomeTimeout:
					timeoutCount++
				default:
					failureCount++
				}
				continue
			}

			sentAt, tsErr := c.store.GetTimestamp(runID, "pipeline_b", p.reqAction, txnID)
			if tsErr != nil {
				// Without sent_at we cannot classify precisely; treat it as failure for counters.
				failureCount++
				ev := &latency.RunLatencyEvent{
					SessionID:   sessionID,
					RunID:       runID,
					Stage:       p.cbStage,
					TxnID:       txnID,
					SentAt:      cutoffAt,
					ReceivedAt: nil,
					LatencyMS:   nil,
					Outcome:     latency.OutcomeFailure,
					RecordedAt:  cutoffAt,
				}
				if err := persist.UpsertRunLatencyEvent(ctx, ev); err != nil {
					log.Printf("[latency] upsert missing event (missing sent_at) FAILED run=%s stage=%s txn=%s error=%v", runID, p.cbStage, txnID, err)
				}
				continue
			}

			outcome := latency.OutcomeFailure
			if cutoffAt.Sub(sentAt) >= callbackTimeout {
				outcome = latency.OutcomeTimeout
				timeoutCount++
			} else {
				failureCount++
			}

			ev := &latency.RunLatencyEvent{
				SessionID:   sessionID,
				RunID:       runID,
				Stage:       p.cbStage,
				TxnID:       txnID,
				SentAt:      sentAt,
				ReceivedAt: nil,
				LatencyMS:   nil,
				Outcome:     outcome,
				RecordedAt:  cutoffAt,
			}
			if err := persist.UpsertRunLatencyEvent(ctx, ev); err != nil {
				log.Printf("[latency] upsert missing event FAILED run=%s stage=%s txn=%s error=%v", runID, p.cbStage, txnID, err)
			}
		}

		avgMs, p90Ms, p95Ms, p99Ms := latency.ComputeSummaryFromSuccessLatenciesMs(successLatenciesMs)

		sum := &latency.RunLatencySummary{
			SessionID:           sessionID,
			RunID:               runID,
			Stage:               p.cbStage,
			TimeoutThresholdMS: timeoutThresholdMS,
			CutoffAt:            cutoffAt,
			Total:               total,
			SuccessCount:        successCount,
			FailureCount:        failureCount,
			TimeoutCount:        timeoutCount,
			AvgMS:               avgMs,
			P90MS:               float64(p90Ms),
			P95MS:               float64(p95Ms),
			P99MS:               float64(p99Ms),
			ComputedAt:         cutoffAt,
		}

		if err := persist.UpsertRunLatencySummary(ctx, sum); err != nil {
			log.Printf("[latency] upsert summary FAILED run=%s stage=%s error=%v", runID, p.cbStage, err)
		}
	}

	return nil
}

// --- Helpers ---

func (c *Controller) updateMetrics(ctx context.Context, runID, action string, results []domainPipeline.DispatchResult) {
	sent := int64(len(results))
	var success, failure int64
	for _, r := range results {
		if r.Err == nil {
			success++
		} else {
			failure++
		}
	}
	_ = c.sessions.IncrMetric(ctx, runID, action, "sent", sent)
	_ = c.sessions.IncrMetric(ctx, runID, action, "success", success)
	_ = c.sessions.IncrMetric(ctx, runID, action, "failure", failure)
}

func (c *Controller) flushRun(runID string) {
	if c.store == nil {
		return
	}

	switch strings.ToUpper(c.cfg.RunPersistence) {
	case "DB":
		bgCtx := context.Background()

		run, err := c.sessions.GetRun(bgCtx, runID)
		if err != nil || run == nil {
			log.Printf("[run] skip DB flush, run not found run=%s error=%v", runID, err)
			c.store.Cleanup(runID)
			return
		}

		persist := c.sessions.Persist()
		if persist == nil {
			log.Printf("[run] persist store not configured, skipping DB flush run=%s", runID)
			c.store.Cleanup(runID)
			return
		}

		err = c.store.Export(runID, func(pipeline, action, txnID string, payload []byte) error {
			direction := "request"
			if strings.HasPrefix(action, "on_") {
				direction = "response"
			}
			return persist.SaveRunPayload(bgCtx, &session.RunPayload{
				RunID:     run.ID,
				SessionID: run.SessionID,
				Stage:     action,
				Direction: direction,
				TxnID:     txnID,
				Status:    0,
				Timestamp: time.Now().UTC(),
				Body:      payload,
			})
		})
		if err != nil {
			log.Printf("[run] DB flush failed run=%s error=%v", runID, err)
		}

	default:
		if !c.cfg.RunsFSEnable {
			c.store.Cleanup(runID)
			return
		}
		if err := c.store.FlushToFilesystem(runID, c.cfg.RunsFSRoot); err != nil {
			log.Printf("[run] flush failed run=%s error=%v", runID, err)
		}
	}

	c.store.Cleanup(runID)
}

func (c *Controller) loadSearchPayload(sess *session.Session) ([]byte, error) {
	return domainPipeline.LoadSearchPayload(c.cfg, sess)
}

func (c *Controller) patchSearchContext(payload []byte, txnID string) ([]byte, error) {
	var full map[string]any
	if err := json.Unmarshal(payload, &full); err != nil {
		return nil, err
	}
	ctxMap, _ := full["context"].(map[string]any)
	if ctxMap == nil {
		ctxMap = map[string]any{}
		full["context"] = ctxMap
	}
	ctxMap["transaction_id"] = txnID
	ctxMap["message_id"] = uuid.NewString()
	ctxMap["timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	if c.cfg != nil && c.cfg.DiscoveryWaitTTLSeconds > 0 {
		ctxMap["ttl"] = fmt.Sprintf("PT%dS", c.cfg.DiscoveryWaitTTLSeconds)
	}
	return json.Marshal(full)
}

func (c *Controller) pollDiscoveryPayload(ctx context.Context, txnID string, timeout time.Duration) []byte {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return nil
		}
		p, err := c.sessions.GetDiscoveryPayload(ctx, txnID)
		if err == nil && len(p) > 0 {
			return p
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func isNack(body []byte) bool {
	var resp struct {
		Message struct {
			Ack struct {
				Status string `json:"status"`
			} `json:"ack"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return false
	}
	return resp.Message.Ack.Status == "NACK"
}

func countSuccess(results []domainPipeline.DispatchResult) int {
	n := 0
	for _, r := range results {
		if r.Err == nil {
			n++
		}
	}
	return n
}

func countErrors(results []domainPipeline.DispatchResult) int {
	n := 0
	for _, r := range results {
		if r.Err != nil {
			n++
		}
	}
	return n
}
