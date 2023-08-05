FROM golang:1.20.7-alpine as builder
WORKDIR /app
COPY main.go .
COPY go.mod .
COPY go.sum .
RUN go mod download
RUN go build -o otel_honeycomb ./app/main.go

FROM alpine:latest AS runner
WORKDIR /home/app
COPY --from=builder /app/otel_honeycomb .
EXPOSE 8099
ENTRYPOINT ["./otel_honeycomb"]