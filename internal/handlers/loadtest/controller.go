package loadtest

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	appconfig "seller_app_load_tester/internal/config"
	lt "seller_app_load_tester/internal/domain/loadtest"
)

//go:embed ui/index.html
var indexHTML []byte

// Controller exposes the load-tester API + dashboard.
type Controller struct {
	cfg   *appconfig.Config
	store *lt.Store
}

func NewController(cfg *appconfig.Config) *Controller {
	return &Controller{cfg: cfg, store: lt.NewStore()}
}

func (c *Controller) Register(app *fiber.App) {
	app.Get("/ui", c.dashboard)
	app.Get("/loadtest/config", c.config)
	app.Post("/loadtest", c.start)
	app.Get("/loadtest/:id", c.status)
	app.Get("/loadtest/:id/ledger", c.ledger)
	app.Post("/loadtest/:id/stop", c.stop)
}

func (c *Controller) dashboard(ctx *fiber.Ctx) error {
	ctx.Set("Content-Type", "text/html; charset=utf-8")
	return ctx.Send(indexHTML)
}

func fail(ctx *fiber.Ctx, status int, code, msg string) error {
	return ctx.Status(status).JSON(fiber.Map{
		"success":   false,
		"error":     fiber.Map{"code": code, "message": msg},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (c *Controller) config(ctx *fiber.Ctx) error {
	return ctx.JSON(fiber.Map{
		"signing_enabled":    c.cfg.BAPPrivateKey != "",
		"bap_id":             c.cfg.BAPID,
		"bap_uri":            c.cfg.BAPURI,
		"max_qps":            c.cfg.LoadTestMaxQPS,
		"max_duration_sec":   c.cfg.LoadTestMaxDurationSec,
		"max_requests":       c.cfg.LoadTestMaxRequests,
		"request_timeout_ms": c.cfg.LoadTestRequestTimeoutMs,
		"slo":                fiber.Map{"metric": "p95", "threshold_ms": 350},
		"defaults":           fiber.Map{"select": 300, "init": 100, "confirm": 20, "duration_sec": 30},
	})
}

func (c *Controller) start(ctx *fiber.Ctx) error {
	var req lt.StartRequest
	if err := json.Unmarshal(ctx.Body(), &req); err != nil {
		return fail(ctx, fiber.StatusBadRequest, "INVALID_BODY", "invalid JSON body: "+err.Error())
	}
	if !actionValid(req.Action) {
		return fail(ctx, fiber.StatusBadRequest, "INVALID_ACTION", "action must be one of search|select|init|confirm")
	}
	if req.BPPURI == "" {
		return fail(ctx, fiber.StatusBadRequest, "MISSING_BPP_URI", "bpp_uri is required")
	}

	// Resolve planned volume.
	planned := req.Count
	if planned <= 0 {
		if req.QPS > 0 && req.DurationSec > 0 {
			planned = req.QPS * req.DurationSec
		} else {
			return fail(ctx, fiber.StatusBadRequest, "MISSING_VOLUME", "provide either count, or qps and duration_sec")
		}
	}
	if req.QPS > c.cfg.LoadTestMaxQPS {
		return fail(ctx, fiber.StatusBadRequest, "QPS_TOO_HIGH", fmt.Sprintf("qps %d exceeds LOADTEST_MAX_QPS %d", req.QPS, c.cfg.LoadTestMaxQPS))
	}
	if req.DurationSec > c.cfg.LoadTestMaxDurationSec {
		return fail(ctx, fiber.StatusBadRequest, "DURATION_TOO_HIGH", fmt.Sprintf("duration_sec %d exceeds max %d", req.DurationSec, c.cfg.LoadTestMaxDurationSec))
	}
	if planned > c.cfg.LoadTestMaxRequests {
		return fail(ctx, fiber.StatusBadRequest, "TOO_MANY_REQUESTS", fmt.Sprintf("planned %d exceeds LOADTEST_MAX_REQUESTS %d", planned, c.cfg.LoadTestMaxRequests))
	}
	if req.Sign && c.cfg.BAPPrivateKey == "" {
		return fail(ctx, fiber.StatusBadRequest, "NO_SIGNING_KEY", "sign=true but BAP_PRIVATE_KEY is not configured")
	}

	// Build the catalog (required for select/init/confirm).
	var cat *lt.Catalog
	if len(req.OnSearch) > 0 {
		parsed, err := lt.ParseCatalog(req.OnSearch)
		if err != nil {
			if req.Action != lt.ActionSearch {
				return fail(ctx, fiber.StatusBadRequest, "INVALID_ON_SEARCH", "on_search could not be parsed: "+err.Error())
			}
		} else {
			cat = parsed
		}
	}
	if cat == nil {
		if req.Action != lt.ActionSearch {
			return fail(ctx, fiber.StatusBadRequest, "MISSING_ON_SEARCH", "on_search catalog is required for select/init/confirm")
		}
		cat = &lt.Catalog{}
	}

	// Context identity: signing keyId must match context.bap_id, so take
	// bap_id/bap_uri from config; domain/country/city from the catalog if present.
	d, cc, city, core := c.contextDefaults(req.OnSearch)
	genCtx := lt.GenContext{
		Domain: d, Country: cc, City: city, CoreVersion: core,
		BAPID: c.cfg.BAPID, BAPURI: c.cfg.BAPURI,
		BPPID: req.BPPID, BPPURI: req.BPPURI,
	}

	seed := time.Now().UnixNano()
	if req.Randomize.Seed != nil {
		seed = *req.Randomize.Seed
	}
	rng := rand.New(rand.NewSource(seed))
	gen := lt.NewGenerator(cat, genCtx, req.Randomize, rng)

	run := &lt.Run{ID: uuid.NewString(), Action: string(req.Action), Planned: planned, Started: time.Now()}
	runCtx, cancel := context.WithCancel(context.Background())
	run.SetCancel(cancel)
	c.store.Add(run)

	runner := lt.NewRunner(
		lt.SigningConfig{Enabled: c.cfg.BAPPrivateKey != "", PrivateKey: c.cfg.BAPPrivateKey, SubscriberID: c.cfg.BAPID, UniqueKeyID: c.cfg.BAPUniqueKeyID},
		c.cfg.MaxInFlight,
		time.Duration(c.cfg.LoadTestRequestTimeoutMs)*time.Millisecond,
	)
	go runner.Execute(runCtx, req, gen, run)

	return ctx.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"run_id": run.ID, "action": run.Action, "planned_requests": planned,
		"target_qps": req.QPS, "started_at": run.Started.UTC().Format(time.RFC3339),
	})
}

func (c *Controller) status(ctx *fiber.Ctx) error {
	run, ok := c.store.Get(ctx.Params("id"))
	if !ok {
		return fail(ctx, fiber.StatusNotFound, "RUN_NOT_FOUND", "no load run with that id")
	}
	return ctx.JSON(run.Snapshot())
}

func (c *Controller) stop(ctx *fiber.Ctx) error {
	run, ok := c.store.Get(ctx.Params("id"))
	if !ok {
		return fail(ctx, fiber.StatusNotFound, "RUN_NOT_FOUND", "no load run with that id")
	}
	run.Stop()
	return ctx.JSON(run.Snapshot())
}

func (c *Controller) ledger(ctx *fiber.Ctx) error {
	run, ok := c.store.Get(ctx.Params("id"))
	if !ok {
		return fail(ctx, fiber.StatusNotFound, "RUN_NOT_FOUND", "no load run with that id")
	}
	rows := run.Ledger()
	if ctx.Query("format") == "csv" {
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"transaction_id", "message_id", "action", "sent_at_utc", "sent_at_unix_ms", "http_status", "ack_status", "client_latency_ms", "error"})
		for _, r := range rows {
			_ = w.Write([]string{r.TransactionID, r.MessageID, r.Action, r.SentAtUTC, strconv.FormatInt(r.SentAtUnixMs, 10), strconv.Itoa(r.HTTPStatus), r.AckStatus, strconv.FormatInt(r.LatencyMs, 10), r.Error})
		}
		w.Flush()
		ctx.Set("Content-Type", "text/csv")
		ctx.Set("Content-Disposition", fmt.Sprintf("attachment; filename=loadtest-%s.csv", run.ID))
		return ctx.Send(buf.Bytes())
	}
	return ctx.JSON(fiber.Map{"run_id": run.ID, "action": run.Action, "count": len(rows), "ledger": rows})
}

func actionValid(a lt.Action) bool {
	switch a {
	case lt.ActionSearch, lt.ActionSelect, lt.ActionInit, lt.ActionConfirm:
		return true
	}
	return false
}

func (c *Controller) contextDefaults(onSearch json.RawMessage) (domain, country, city, core string) {
	domain, country, city, core = c.cfg.Domain, c.cfg.CountryCode, c.cfg.CityCode, c.cfg.CoreVersion
	if len(onSearch) == 0 {
		return
	}
	var env struct {
		Context struct {
			Domain      string `json:"domain"`
			Country     string `json:"country"`
			City        string `json:"city"`
			CoreVersion string `json:"core_version"`
		} `json:"context"`
	}
	if json.Unmarshal(onSearch, &env) == nil {
		if env.Context.Domain != "" {
			domain = env.Context.Domain
		}
		if env.Context.Country != "" {
			country = env.Context.Country
		}
		if env.Context.City != "" {
			city = env.Context.City
		}
		if env.Context.CoreVersion != "" {
			core = env.Context.CoreVersion
		}
	}
	return
}
