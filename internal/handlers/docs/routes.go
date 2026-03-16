package docs

import "github.com/gofiber/fiber/v2"

const swaggerHTML = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>Seller Load Tester API</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.onload = () => {
        window.ui = SwaggerUIBundle({
          url: "/swagger/openapi.yaml",
          dom_id: "#swagger-ui",
        });
      };
    </script>
  </body>
</html>`

func Register(app *fiber.App) {
	app.Get("/swagger", func(ctx *fiber.Ctx) error {
		ctx.Type("html")
		return ctx.SendString(swaggerHTML)
	})

	app.Get("/swagger/openapi.yaml", func(ctx *fiber.Ctx) error {
		return ctx.SendFile("docs/openapi.yaml")
	})
}

