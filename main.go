package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/emanuelef/go-fiber-honeycomb/otel_instrumentation"
	_ "github.com/joho/godotenv/autoload"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/go-resty/resty/v2"
	"github.com/gofiber/contrib/otelfiber"

	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	externalURL     = "https://pokeapi.co/api/v2/pokemon/ditto"
	secondaryAppURL = "http://localhost:8082/hello"
)

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

	app.Use(otelfiber.Middleware(otelfiber.WithNext(func(c *fiber.Ctx) bool {
		return c.Path() == "/health"
	})))

	app.Use(recover.New())
	app.Use(cors.New())
	app.Use(compress.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Send(nil)
	})

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

		resp, err = otelhttp.Get(c.UserContext(), secondaryAppURL)
		_, _ = io.ReadAll(resp.Body)

		if err != nil {
			return fiber.ErrInternalServerError
		}

		// Get current span and add new attributes
		span := trace.SpanFromContext(c.UserContext())
		span.SetAttributes(attribute.Bool("isTrue", true), attribute.String("stringAttr", "Ciao"))

		// Create a child span
		ctx, childSpan := tracer.Start(c.UserContext(), "custom-span")
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

	app.Get("/hello-http", func(c *fiber.Ctx) error {
		client := http.Client{
			Transport: otelhttp.NewTransport(
				http.DefaultTransport,
				otelhttp.WithClientTrace(func(ctx context.Context) *httptrace.ClientTrace {
					return otelhttptrace.NewClientTrace(ctx)
				})),
		}

		req, err := http.NewRequestWithContext(c.UserContext(), "GET", externalURL, nil)
		if err != nil {
			return err
		}

		otel.GetTextMapPropagator().Inject(c.UserContext(), propagation.HeaderCarrier(req.Header))

		resp, _ := client.Do(req)
		_, _ = io.ReadAll(resp.Body)

		req, err = http.NewRequestWithContext(c.UserContext(), "GET", externalURL, nil)
		if err != nil {
			return err
		}
		resp, _ = client.Do(req)
		body, _ := io.ReadAll(resp.Body)
		result := []map[string]any{}
		_ = json.Unmarshal([]byte(body), &result)

		return c.SendString(resp.Status)
	})

	app.Get("/hello-resty", func(c *fiber.Ctx) error {
		// client := resty.New()

		// get current span
		span := trace.SpanFromContext(c.UserContext())

		// add events to span
		time.Sleep(100 * time.Millisecond)
		span.AddEvent("Done first fake long running task")
		time.Sleep(150 * time.Millisecond)
		span.AddEvent("Done second fake long running task")

		client := resty.NewWithClient(
			&http.Client{
				Transport: otelhttp.NewTransport(http.DefaultTransport,
					otelhttp.WithClientTrace(func(ctx context.Context) *httptrace.ClientTrace {
						return otelhttptrace.NewClientTrace(ctx)
					})),
			},
		)

		restyReq := client.R()
		restyReq.SetContext(c.UserContext())

		// Needed to propagate the trace remotely
		otel.GetTextMapPropagator().Inject(c.UserContext(), propagation.HeaderCarrier(restyReq.Header))

		resp, _ := restyReq.Get(externalURL)

		_, _ = restyReq.Get(externalURL)

		// simulate some post processing
		time.Sleep(50 * time.Millisecond)

		return c.SendString(resp.Status())
	})

	// This is to generate a new span that is not a descendand of an existing one
	go func() {
		for range time.Tick(time.Minute) {
			ctx, span := tracer.Start(context.Background(), "timed-operation")
			resp, _ := otelhttp.Get(ctx, externalURL)
			_, _ = io.ReadAll(resp.Body)
			span.End()
		}
	}()

	err = app.Listen("0.0.0.0:8080")
	if err != nil {
		log.Panic(err)
	}
}
