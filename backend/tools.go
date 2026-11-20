//go:build tools

// Package tools pins dependencies that later tasks import, so go mod tidy keeps them.
package tools

import (
	_ "github.com/aws/aws-sdk-go-v2"
	_ "github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	_ "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	_ "pgregory.net/rapid"
)
