# go-fiber-honeycomb

[![Golangci Lint Check](https://github.com/emanuelef/go-fiber-honeycomb/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/emanuelef/go-fiber-honeycomb/actions/workflows/golangci-lint.yml)

## Introduction

This the repo for the Medium Article Start using OpenTelemetry with Go Fiber.  
The aim is to show how to instrument with [OpenTelemetry](https://opentelemetry.io) a web server written in Go using the [Fiber framework](https://gofiber.io).  
In order to see the traces generated [Honeycomb.io](https://www.honeycomb.io) is used as the Observability solution.

## Run code

The short video will show how to start the 3 apps with docker compose and run some http requests.

https://github.com/emanuelef/go-fiber-honeycomb/assets/48717/0f7938f7-e08e-4de2-93e2-a2b2e8026b47

First you need to get an API token from Honeycomb.io, you can sign up [here](https://ui.honeycomb.io/signup), the free plan is very generous, doesn't require to set up any payment and is time unlimited.  

You can then run the example locally or using Codespaces, the steps are the same.  

In order to populate the .env files needed by the the three apps run:  
```shell
./set_token.sh
```

And paste the API Key from Honeycomb.io, it will appear at the end of the sign up or can be retrieved later in the Account > Team settings. 

There are three apps to demonstrate the distributed tracing, to start them altogher run:

```shell
docker compose up --build
```
Or they can be run independently with 
```shell
go run main.go
```
from each folder: 

- Main app runs on port 8080
- Secondary app on port 8082
- gRPC server on port 7070


```shell
curl http://127.0.0.1:8080/hello
```

To run all the endpoints implemented in the main app:
```shell
./run_http_requests.sh
```

Fome Home you will see the traces appearing at the bottom (or you can run a query)

<img width="500" alt="Honeycomb Home" src="https://github.com/emanuelef/go-fiber-honeycomb/assets/48717/86adce7b-332e-41e3-ba3a-720dd671c586">  

Clicking on the button on the left will open the Traces view

<img width="500" alt="Honecomb resty traces" src="https://github.com/emanuelef/go-fiber-honeycomb/assets/48717/186a6c56-38f1-47c2-9792-fa98a20ef980">