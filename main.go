package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/go-resty/resty/v2"
	"github.com/gofiber/contrib/otelfiber"

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
	meter := otel.Meter("my-meter")

	requestCounter, _ := meter.Int64Counter("hello_request_counter")

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
		requestCounter.Add(c.UserContext(), 1)

		resp, err := otelhttp.Get(c.UserContext(), "https://pokeapi.co/api/v2/pokemon/ditto")
		if err != nil {
			return fiber.ErrInternalServerError
		}

		resp, err = otelhttp.Get(c.UserContext(), "https://pokeapi.co/api/v2/pokemon/ditto")

		if err != nil {
			return fiber.ErrInternalServerError
		}

		// Get current span and add new attributes
		span := trace.SpanFromContext(c.UserContext())
		span.SetAttributes(attribute.Bool("isTrue", true), attribute.String("stringAttr", "Ciao"))

		// Create a child span
		_, childSpan := tracer.Start(c.UserContext(), "custom-span")
		time.Sleep(1 * time.Second)
		childSpan.End()

		time.Sleep(1 * time.Second)

		// Add an event to the current span
		span.AddEvent("Done Activity")

		return c.SendString(resp.Status)
	})

	app.Get("/hello-http", func(c *fiber.Ctx) error {
		client := http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		}

		req, err := http.NewRequestWithContext(c.UserContext(), "GET", "https://pokeapi.co/api/v2/pokemon/ditto", nil)
		if err != nil {
			return err
		}
		resp, _ := client.Do(req)

		req, err = http.NewRequestWithContext(c.UserContext(), "GET", "https://pokeapi.co/api/v2/pokemon/ditto", nil)
		if err != nil {
			return err
		}
		resp, _ = client.Do(req)

		// otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(restyReq.Header))

		return c.SendString(resp.Status)
	})

	app.Get("/hello-resty", func(c *fiber.Ctx) error {
		//client := resty.New()

		span := trace.SpanFromContext(c.UserContext())
		time.Sleep(1 * time.Second)
		span.AddEvent("Done Fake long running task")

		client := resty.NewWithClient(
			&http.Client{
				Transport: otelhttp.NewTransport(http.DefaultTransport),
			},
		)

		restyReq := client.R()
		restyReq.SetContext(c.UserContext())
		otel.GetTextMapPropagator().Inject(c.UserContext(), propagation.HeaderCarrier(restyReq.Header))

		resp, _ := restyReq.
			EnableTrace().
			Get("https://pokeapi.co/api/v2/pokemon/ditto")

		return c.SendString(resp.Status())
	})

	err = app.Listen("127.0.0.1:8099")
	if err != nil {
		log.Panic(err)
	}
}
