package storage

import (
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// DynamoDBProvider is a KVProvider that hands out per-module stores backed by
// a single shared DynamoDB table. Module isolation is by partition (pk =
// moduleName); no key-prefix wrapping is needed at this layer.
type DynamoDBProvider struct {
	client *dynamodb.Client
	table  string
}

// NewDynamoDBProvider returns a provider over the given client + table.
// The client is goroutine-safe and outlives every store handed out by For.
func NewDynamoDBProvider(client *dynamodb.Client, table string) *DynamoDBProvider {
	return &DynamoDBProvider{client: client, table: table}
}

// For returns a DynamoDBKVStore partitioned by moduleName. moduleName is
// re-validated against the same alphabet enforced by FirestoreProvider so
// behaviour is identical between backends. An invalid name yields an
// invalidStore — every op errors at first use, surfacing the bug early.
func (p *DynamoDBProvider) For(moduleName string) KVStore {
	if !collectionNameRe.MatchString(moduleName) {
		return invalidStore{name: moduleName}
	}
	return NewDynamoDBKVStore(p.client, p.table, moduleName)
}
