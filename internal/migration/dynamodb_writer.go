package migration

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDBWriter writes migrated KV records into the runtime DynamoDB table
// using the exact attribute shape internal/storage/dynamodb_kv.go expects:
// pk (S), sk (S), value (S), updatedAt (N).
//
// Idempotency: by default the writer attaches a ConditionExpression that
// rejects writes where the (pk, sk) pair already exists. The CLI exposes an
// --overwrite flag that drops the condition for explicit re-imports.
type DynamoDBWriter struct {
	client    *dynamodb.Client
	table     string
	overwrite bool
}

func NewDynamoDBWriter(client *dynamodb.Client, table string, overwrite bool) *DynamoDBWriter {
	return &DynamoDBWriter{client: client, table: table, overwrite: overwrite}
}

// ErrItemExists signals a guarded write skipped because (pk, sk) was already
// present. The caller increments the "skipped (already imported)" counter.
var ErrItemExists = errors.New("dynamodb: item exists")

// Put writes one record. value bytes are stored as a DynamoDB String so the
// payload is human-readable in the AWS console; all current sources are JSON
// and therefore UTF-8 safe.
func (w *DynamoDBWriter) Put(ctx context.Context, pk, sk string, value []byte) error {
	in := &dynamodb.PutItemInput{
		TableName: aws.String(w.table),
		Item: map[string]types.AttributeValue{
			"pk":        &types.AttributeValueMemberS{Value: pk},
			"sk":        &types.AttributeValueMemberS{Value: sk},
			"value":     &types.AttributeValueMemberS{Value: string(value)},
			"updatedAt": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().UTC().UnixNano(), 10)},
		},
	}
	if !w.overwrite {
		in.ConditionExpression = aws.String("attribute_not_exists(pk)")
	}
	_, err := w.client.PutItem(ctx, in)
	if err != nil {
		var cf *types.ConditionalCheckFailedException
		if errors.As(err, &cf) {
			return ErrItemExists
		}
		return fmt.Errorf("dynamodb put %s/%s: %w", pk, sk, err)
	}
	return nil
}
