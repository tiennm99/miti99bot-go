package storage

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// dynamoDBHTTPTimeout caps individual SDK HTTP calls. DynamoDB on-demand is
// fast (typically <50ms); a 10s budget absorbs cold-start TLS handshake on
// Lambda + retries without hiding pathological hangs.
const dynamoDBHTTPTimeout = 10 * time.Second

// NewDynamoDBClient constructs a DynamoDB client using AWS standard credential
// resolution: Lambda execution role → env vars → shared config. Region is
// resolved by the SDK from AWS_REGION (set by Lambda) or AWS_DEFAULT_REGION.
//
// The caller may pass a non-empty endpoint override (e.g. "http://localhost:8000")
// to point at DynamoDB Local for tests. An empty endpoint uses the AWS default.
func NewDynamoDBClient(ctx context.Context, endpoint string) (*dynamodb.Client, error) {
	httpClient := &http.Client{Timeout: dynamoDBHTTPTimeout}

	loadOpts := []func(*config.LoadOptions) error{
		config.WithHTTPClient(httpClient),
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load AWS config: %w", err)
	}

	clientOpts := []func(*dynamodb.Options){}
	if endpoint != "" {
		clientOpts = append(clientOpts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}
	return dynamodb.NewFromConfig(cfg, clientOpts...), nil
}

// DynamoDBEndpointFromEnv returns the override endpoint for tests / local dev.
// Empty string means "use AWS default endpoint."
func DynamoDBEndpointFromEnv() string {
	return os.Getenv("DYNAMODB_LOCAL_URL")
}
