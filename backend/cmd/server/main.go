// Command server runs the RukPay API. It serves the shared handler with
// net/http on the port from PORT, or through the Lambda adapter when it runs as
// a Lambda function, so one binary works in both places (Requirements 10.2,
// 10.6).
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"

	"github.com/RahulKumar1119/rukpay/backend/internal/httpapi"
)

// readHeaderTimeout bounds how long a client may take to send its request
// headers. There is no write timeout, because the event stream stays open.
const readHeaderTimeout = 10 * time.Second

func main() {
	cfg, err := httpapi.LoadConfig(os.Getenv)
	if err != nil {
		fail(err)
	}
	handler, err := httpapi.Build(context.Background(), cfg)
	if err != nil {
		fail(err)
	}

	if cfg.OnLambda {
		lambda.Start(httpadapter.NewV2(handler).ProxyWithContext) // Function URL, payload v2
		return
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	fmt.Fprintln(os.Stdout, "RukPay is listening on port", cfg.Port)
	if err := srv.ListenAndServe(); err != nil {
		fail(err)
	}
}

// fail prints the error and exits with a non-zero status. The errors that reach
// it never hold the signing key.
func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
