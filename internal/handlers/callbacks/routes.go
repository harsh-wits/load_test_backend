package callbacks

import (
	"github.com/gofiber/fiber/v2"

	domainPipeline "seller_app_load_tester/internal/domain/pipeline"
	"seller_app_load_tester/internal/domain/session"
	"seller_app_load_tester/internal/shared/runlog"
)

type VerificationConfig struct {
	Enabled   bool
	PublicKey string
}

type Controller struct {
	store        runlog.Store
	sessions     *session.Manager
	notifier     *domainPipeline.CallbackNotifier
	validator    domainPipeline.CallbackValidator
	verification VerificationConfig
}

func NewController(
	store runlog.Store,
	sessions *session.Manager,
	notifier *domainPipeline.CallbackNotifier,
	validator domainPipeline.CallbackValidator,
	verification VerificationConfig,
) *Controller {
	return &Controller{
		store:        store,
		sessions:     sessions,
		notifier:     notifier,
		validator:    validator,
		verification: verification,
	}
}

func (c *Controller) Register(app *fiber.App) {
	app.Post("/on_search", c.onSearch_handler)
	app.Post("/on_select", c.onSelect)
	app.Post("/on_init", c.onInit)
	app.Post("/on_confirm", c.onConfirm)
}
