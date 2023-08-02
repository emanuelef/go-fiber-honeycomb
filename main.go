package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"os"
	"time"

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
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

const externalURL = "https://pokeapi.co/api/v2/pokemon/ditto"

var serviceName = os.Getenv("OTEL_SERVICE_NAME")

var tracer trace.Tracer

func main() {
	ctx := context.Background()

	// Configure a new OTLP exporter using environment variables for sending data to Honeycomb over gRPC
	clientOTel := otlptracegrpc.NewClient()
	exp, err := otlptrace.New(ctx, clientOTel)
	if err != nil {
		log.Fatalf("failed to initialize exporter: %e", err)
	}

	resource, rErr := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			attribute.String("environment", "test"),
		),
	)

	if rErr != nil {
		panic(rErr)
	}

	// Create a new tracer provider with a batch span processor and the otlp exporter
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resource),
	)

	// Handle shutdown to ensure all sub processes are closed correctly and telemetry is exported
	defer func() {
		_ = exp.Shutdown(ctx)
		_ = tp.Shutdown(ctx)
	}()

	// Register the global Tracer provider
	otel.SetTracerProvider(tp)

	// Register the W3C trace context and baggage propagators so data is propagated across services/processes
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	tracer = tp.Tracer(serviceName)

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

	customCtx, spanMain := tracer.Start(context.Background(), "custom-span-main")
	resp, _ := otelhttp.Get(customCtx, externalURL)
	_, _ = io.ReadAll(resp.Body)
	spanMain.End()

	log.Println(resp.Status)

	err = app.Listen("127.0.0.1:8099")
	if err != nil {
		log.Panic(err)
	}
}
