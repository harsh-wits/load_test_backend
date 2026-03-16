package di

import (
	"context"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"

	appconfig "seller_app_load_tester/internal/config"
	domainPipeline "seller_app_load_tester/internal/domain/pipeline"
	domainSession "seller_app_load_tester/internal/domain/session"
	callbackHandlers "seller_app_load_tester/internal/handlers/callbacks"
	docsHandlers "seller_app_load_tester/internal/handlers/docs"
	testingHandlers "seller_app_load_tester/internal/handlers/testing"
	sessionPorts "seller_app_load_tester/internal/ports/session"
	"seller_app_load_tester/internal/ports/seller"
	appMongo "seller_app_load_tester/internal/shared/mongo"
	"seller_app_load_tester/internal/shared/redis"
	"seller_app_load_tester/internal/shared/runlog"
)

type Container struct {
	cfg         *appconfig.Config
	redis       redis.Client
	mongo       *appMongo.Client
	store       runlog.Store
	sessions    *domainSession.Manager
	rateLimiter *domainSession.RateLimiter
}

func BuildContainer() (*Container, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	redisClient, err := redis.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("init redis: %w", err)
	}

	mongoClient, err := appMongo.NewClient(cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return nil, fmt.Errorf("init mongo: %w", err)
	}

	var store runlog.Store
	switch cfg.RunStoreBackend {
	case "redis":
		store = runlog.NewRedisStore(redisClient, cfg.RunPayloadTTLSeconds)
		log.Printf("[di] using Redis run payload store (ttl=%ds)", cfg.RunPayloadTTLSeconds)
	default:
		store = runlog.NewMemoryStore()
		log.Printf("[di] using in-memory run payload store")
	}

	redisSessionStore := sessionPorts.NewRedisStore(redisClient, cfg.SessionTTLSeconds)
	mongoSessionStore := sessionPorts.NewMongoStore(mongoClient)
	sessionMgr := domainSession.NewManager(redisSessionStore, mongoSessionStore, cfg.SessionTTLSeconds)

	rl := domainSession.NewRateLimiter(redisClient, cfg.GlobalRPSLimit, cfg.PerSessionRPSLimit)
	if err := rl.Init(context.Background()); err != nil {
		log.Printf("[di] rate limiter init failed (non-fatal): %v", err)
	}

	return &Container{
		cfg:         cfg,
		redis:       redisClient,
		mongo:       mongoClient,
		store:       store,
		sessions:    sessionMgr,
		rateLimiter: rl,
	}, nil
}

func (c *Container) Config() *appconfig.Config {
	return c.cfg
}

func (c *Container) RegisterRoutes(app *fiber.App) error {
	app.Get("/health", func(ctx *fiber.Ctx) error {
		if err := c.redis.Ping(ctx.Context()); err != nil {
			return ctx.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "degraded", "error": err.Error(),
			})
		}
		return ctx.JSON(fiber.Map{"status": "ok"})
	})

	if c.cfg.SwaggerEnable {
		docsHandlers.Register(app)
	}

	selectBatch := domainPipeline.NewSelectBatchService()
	sellerClient := seller.NewHTTPClient(seller.SigningConfig{
		PrivateKey:   c.cfg.BAPPrivateKey,
		SubscriberID: c.cfg.BAPID,
		UniqueKeyID:  c.cfg.BAPUniqueKeyID,
		Enabled:      c.cfg.BAPPrivateKey != "",
	})

	notifier := domainPipeline.NewCallbackNotifier()
	bCoordinator := domainPipeline.NewBCoordinator(selectBatch, c.store, sellerClient, c.cfg)

	testingHandlers.NewController(
		c.cfg, c.sessions, bCoordinator, sellerClient,
		notifier, c.store, c.rateLimiter,
	).Register(app)

	callbackHandlers.NewController(
		c.store, c.sessions, notifier,
		domainPipeline.NoopValidator{},
		callbackHandlers.VerificationConfig{
			Enabled:   c.cfg.VerificationEnable,
			PublicKey: c.cfg.BAPPublicKey,
		},
	).Register(app)

	return nil
}

func (c *Container) Close() error {
	var firstErr error
	if err := c.redis.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if c.mongo != nil {
		if err := c.mongo.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
