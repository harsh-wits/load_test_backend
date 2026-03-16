package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"

	"seller_app_load_tester/internal/config/di"
	"seller_app_load_tester/internal/shared/apierror"
)

func main() {
	container, err := di.BuildContainer()
	if err != nil {
		log.Fatalf("failed to build container: %v", err)
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: apierror.ErrorHandler(),
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "Content-Type,Authorization",
	}))

	if err := container.RegisterRoutes(app); err != nil {
		log.Fatalf("failed to register routes: %v", err)
	}

	go func() {
		if err := app.Listen(":" + container.Config().HTTPPort); err != nil {
			log.Fatalf("fiber server error: %v", err)
		}
	}()

	// graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	if err := app.Shutdown(); err != nil {
		log.Printf("error during shutdown: %v", err)
	}

	if err := container.Close(); err != nil {
		log.Printf("error closing container: %v", err)
	}
}

