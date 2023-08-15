package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"os"
	"time"

	"github.com/emanuelef/go-fiber-honeycomb/otel_instrumentation"
	protos "github.com/emanuelef/go-fiber-honeycomb/proto"
	_ "github.com/joho/godotenv/autoload"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/go-resty/resty/v2"
	"github.com/gofiber/contrib/otelfiber"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	externalURL = "https://pokeapi.co/api/v2/pokemon/ditto"
)

var tracer trace.Tracer

func init() {
	tracer = otel.Tracer("github.com/emanuelef/go-fiber-honeycomb")
}

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

// The context will carry the traceid and span id
// so once is passed it can get be used access to the current span
// or create a child one
func exampleChildSpan(ctx context.Context) {
	_, anotherSpan := tracer.Start(ctx, "operation-name")
	anotherSpan.AddEvent("ciao")
	time.Sleep(10 * time.Millisecond)
	anotherSpan.End()
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
		return c.Send(nil)
	})

	app.Get("/hello-otelhttp", func(c *fiber.Ctx) error {
		resp, err := otelhttp.Get(c.UserContext(), externalURL)

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		_, _ = io.ReadAll(resp.Body) // This is needed to close the span

		secondaryHost := getEnv("SECONDARY_HOST", "localhost")
		secondaryAddress := fmt.Sprintf("http://%s:8082", secondaryHost)
		secondaryHelloUrl := fmt.Sprintf("%s/hello", secondaryAddress)

		// make sure secondary app is running
		resp, err = otelhttp.Get(c.UserContext(), secondaryHelloUrl)

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		_, _ = io.ReadAll(resp.Body) // This is needed to close the span

		// Get current span and add new attributes
		span := trace.SpanFromContext(c.UserContext())
		span.SetAttributes(attribute.Bool("isTrue", true), attribute.String("stringAttr", "Ciao"))

		// Create a child span
		ctx, childSpan := tracer.Start(c.UserContext(), "custom-span")
		time.Sleep(10 * time.Millisecond)
		resp, _ = otelhttp.Get(ctx, externalURL)
		_, _ = io.ReadAll(resp.Body)
		defer childSpan.End()

		time.Sleep(20 * time.Millisecond)

		// Add an event to the current span
		span.AddEvent("Done Activity")
		exampleChildSpan(ctx)
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
		// get current span
		span := trace.SpanFromContext(c.UserContext())

		// add events to span
		time.Sleep(70 * time.Millisecond)
		span.AddEvent("Done first fake long running task")
		time.Sleep(90 * time.Millisecond)
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
		restyReq.SetContext(c.UserContext()) // makes it possible to use the HTTP request trace_id

		// Needed to propagate the traceparent remotely
		otel.GetTextMapPropagator().Inject(c.UserContext(), propagation.HeaderCarrier(restyReq.Header))

		// run HTTP request first time
		resp, _ := restyReq.Get(externalURL)

		// run second time and notice http.getconn time compared to first one
		_, _ = restyReq.Get(externalURL)

		// simulate some post processing
		span.AddEvent("Start post processing")
		time.Sleep(50 * time.Millisecond)

		return c.SendString(resp.Status())
	})

	app.Get("/hello-grpc", func(c *fiber.Ctx) error {
		grpcHost := getEnv("GRPC_TARGET", "localhost")
		grpcTarget := fmt.Sprintf("%s:7070", grpcHost)

		conn, err := grpc.Dial(grpcTarget,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()))
		if err != nil {
			log.Printf("Did not connect: %v", err)
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		defer conn.Close()
		cli := protos.NewGreeterClient(conn)

		r, err := cli.SayHello(c.UserContext(), &protos.HelloRequest{Greeting: "ciao"})
		if err != nil {
			log.Printf("Error: %v", err)
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}

		log.Printf("Greeting: %s", r.GetReply())

		return c.Send(nil)
	})

	// This is to generate a new span that is not a descendand of an existing one
	go func() {
		// in a real app it is better to use time.NewTicker so that the tivker will be
		// recovered by garbage collector
		for range time.Tick(time.Minute) {
			ctx, span := tracer.Start(context.Background(), "timed-operation")
			resp, _ := otelhttp.Get(ctx, externalURL)
			_, _ = io.ReadAll(resp.Body)
			span.End()
		}
	}()

	host := getEnv("HOST", "localhost")
	port := getEnv("PORT", "8080")
	hostAddress := fmt.Sprintf("%s:%s", host, port)

	err = app.Listen(hostAddress)
	if err != nil {
		log.Panic(err)
	}
}
