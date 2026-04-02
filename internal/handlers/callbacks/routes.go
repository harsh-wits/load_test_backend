package callbacks

import (
	"context"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"

	"seller_app_load_tester/internal/domain/latency"
	domainPipeline "seller_app_load_tester/internal/domain/pipeline"
	"seller_app_load_tester/internal/domain/session"
	"seller_app_load_tester/internal/ports/registry"
	"seller_app_load_tester/internal/shared/runlog"
)

type VerificationConfig struct {
	RegistryBaseURL         string
	RegistryCacheTTLSeconds int
	SigningPrivateKey       string
	SigningSubscriberID     string
	SigningUniqueKeyID      string
}

type Controller struct {
	store        runlog.Store
	sessions     *session.Manager
	notifier     *domainPipeline.CallbackNotifier
	validator    domainPipeline.CallbackValidator
	verification VerificationConfig
	registry     registry.Client
	latencyQueue chan *latency.RunLatencyEvent
	queueDropped atomic.Int64
	queueSyncFallback atomic.Int64
	queueMaxDepth atomic.Int64
}

func NewController(
	store runlog.Store,
	sessions *session.Manager,
	notifier *domainPipeline.CallbackNotifier,
	validator domainPipeline.CallbackValidator,
	verification VerificationConfig,
) *Controller {
	reg := registry.NewHTTPClient(
		verification.RegistryBaseURL,
		verification.SigningPrivateKey,
		verification.SigningSubscriberID,
		verification.SigningUniqueKeyID,
		time.Duration(verification.RegistryCacheTTLSeconds)*time.Second,
	)
	c := &Controller{
		store:        store,
		sessions:     sessions,
		notifier:     notifier,
		validator:    validator,
		verification: verification,
		registry:     reg,
		latencyQueue: make(chan *latency.RunLatencyEvent, readLatencyQueueSize()),
	}
	c.startLatencyWorkers(readLatencyWorkerCount())
	return c
}

func readLatencyQueueSize() int {
	v := os.Getenv("CALLBACK_LATENCY_QUEUE_SIZE")
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 2048
	}
	return n
}

func readLatencyWorkerCount() int {
	v := os.Getenv("CALLBACK_LATENCY_WORKERS")
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 4
	}
	return n
}

func (c *Controller) startLatencyWorkers(n int) {
	if c == nil || c.latencyQueue == nil || n <= 0 {
		return
	}
	for i := 0; i < n; i++ {
		go c.latencyWorker(context.Background(), i+1)
	}
}

func (c *Controller) latencyWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-c.latencyQueue:
			if ev == nil || c.sessions == nil || c.sessions.Persist() == nil {
				continue
			}
			if err := c.sessions.Persist().UpsertRunLatencyEvent(context.Background(), ev); err != nil {
				log.Printf("[callbacks] async latency upsert FAILED worker=%d run=%s txn=%s stage=%s error=%v", workerID, ev.RunID, ev.TxnID, ev.Stage, err)
			}
		}
	}
}

func (c *Controller) enqueueLatencyEvent(ev *latency.RunLatencyEvent) bool {
	if c == nil || c.latencyQueue == nil || ev == nil {
		return false
	}
	select {
	case c.latencyQueue <- ev:
		depth := int64(len(c.latencyQueue))
		for {
			cur := c.queueMaxDepth.Load()
			if depth <= cur {
				break
			}
			if c.queueMaxDepth.CompareAndSwap(cur, depth) {
				break
			}
		}
		return true
	default:
		drops := c.queueDropped.Add(1)
		if drops == 1 || drops%100 == 0 {
			log.Printf("[callbacks] latency queue full drops=%d depth=%d cap=%d", drops, len(c.latencyQueue), cap(c.latencyQueue))
		}
		c.queueSyncFallback.Add(1)
		return false
	}
}

func (c *Controller) Register(app *fiber.App) {
	app.Post("/on_search", c.onSearch_handler)
	app.Post("/on_select", c.onSelect)
	app.Post("/on_init", c.onInit)
	app.Post("/on_confirm", c.onConfirm)
}
