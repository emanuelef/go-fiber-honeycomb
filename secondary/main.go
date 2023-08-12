package main

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/emanuelef/go-fiber-honeycomb/otel_instrumentation"
	_ "github.com/joho/godotenv/autoload"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/gofiber/contrib/otelfiber"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const externalURL = "https://pokeapi.co/api/v2/pokemon/ditto"

var tracer trace.Tracer

func init() {
	tracer = otel.Tracer("github.com/emanuelef/go-fiber-honeycomb")
}

func main() {
	ctx := context.Background()
	tp, exp, err := otel_instrumentation.InitializeGlobalTracerProvider(ctx)

	// Handle shutdown to ensure all sub processes are closed correctly and telemetry is exported
	defer func() {
		_ = exp.Shutdown(ctx)
		_ = tp.Shutdown(ctx)
	}()

	if err != nil {
		log.Fatalf("failed to initialize OpenTelemetry: %e", err)
	}

	app := fiber.New()

	app.Use(otelfiber.Middleware())

	app.Use(recover.New())
	app.Use(cors.New())
	app.Use(compress.New())

	app.Get("/hello", func(c *fiber.Ctx) error {
		_, err := otelhttp.Get(c.UserContext(), externalURL)
		if err != nil {
			return fiber.ErrInternalServerError
		}

		resp, err := otelhttp.Get(c.UserContext(), externalURL)
		_, _ = io.ReadAll(resp.Body)

		if err != nil {
			return fiber.ErrInternalServerError
		}

		// Get current span and add new attributes
		span := trace.SpanFromContext(c.UserContext())
		span.SetAttributes(attribute.Bool("isTrue", true), attribute.String("stringAttr", "Ciao"))

		// Create a child span
		ctx, childSpan := tracer.Start(c.UserContext(), "custom-span-secondary")
		time.Sleep(10 * time.Millisecond)
		resp, _ = otelhttp.Get(ctx, externalURL)
		_, _ = io.ReadAll(resp.Body)
		childSpan.End()

		time.Sleep(20 * time.Millisecond)

		// Add an event to the current span
		span.AddEvent("Done Activity")

		_, span = tracer.Start(ctx, "operation-name")
		span.AddEvent("ciao")
		span.End()

		return c.SendString(resp.Status)
	})

	err = app.Listen("0.0.0.0:8082")
	if err != nil {
		log.Panic(err)
	}
}
