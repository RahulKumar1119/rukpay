// Command lambda runs the RukPay API as an AWS Lambda function behind a Function
// URL. It builds the same handler as cmd/server and serves it through the
// httpadapter (Requirement 10.3). Build it with -tags lambda.norpc for the
// provided.al2023 runtime.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"

	"github.com/RahulKumar1119/rukpay/backend/internal/httpapi"
)

func main() {
	cfg, err := httpapi.LoadConfig(os.Getenv)
	if err != nil {
		fail(err)
	}
	// The function runs on Lambda whatever the environment says, so the stream
	// closes early and the client polls (Requirements 8.6, 8.7).
	cfg.OnLambda = true

	handler, err := httpapi.Build(context.Background(), cfg)
	if err != nil {
		fail(err)
	}
	lambda.Start(httpadapter.NewV2(handler).ProxyWithContext) // Function URL, payload v2
}

// fail prints the error and exits with a non-zero status. The errors that reach
// it never hold the signing key.
func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
