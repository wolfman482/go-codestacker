package routes

import (
	"fiber-postgres-docker/handlers"

	"github.com/gofiber/fiber/v2"
)

func SetupRoutes(app *fiber.App) {
	app.Post("/register", handlers.Register)
	app.Post("/login", handlers.Login)

	app.Use(handlers.AuthMiddleware())

	app.Post("/upload", handlers.UploadPDF)
	app.Get("/pdfs", handlers.ListPDFs)
	app.Get("/pdfs/:id", handlers.GetPDF)
	app.Get("/search", handlers.SearchWord)
	app.Get("/pdfs/:id/page/:page", handlers.GetPageAsImage)
	app.Delete("/pdfs/:id", handlers.DeletePDF)
}
