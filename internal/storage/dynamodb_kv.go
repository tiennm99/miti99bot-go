package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDB attribute names. `value` is reserved by DynamoDB expressions, so
// every reference goes through ExpressionAttributeNames (#v).
const (
	dynamoPKAttr        = "pk"
	dynamoSKAttr        = "sk"
	dynamoValueAttr     = "value"
	dynamoUpdatedAtAttr = "updatedAt"
)

// DynamoDBKVStore is a KVStore backed by a single DynamoDB table with a
// composite key (pk, sk). FirestoreProvider uses one collection per module;
// DynamoDBProvider uses one partition (pk = moduleName) per module — the
// table itself is shared. The user-supplied key becomes the sort key, so
// List(prefix) maps to a Query with begins_with(sk, prefix).
type DynamoDBKVStore struct {
	client     *dynamodb.Client
	table      string
	moduleName string
}

// NewDynamoDBKVStore returns a store partition-scoped to moduleName.
// Callers must validate moduleName beforehand (DynamoDBProvider does).
func NewDynamoDBKVStore(client *dynamodb.Client, table, moduleName string) *DynamoDBKVStore {
	return &DynamoDBKVStore{client: client, table: table, moduleName: moduleName}
}

// Get returns the raw bytes stored at key, or ErrNotFound. Strong read for
// parity with the Firestore impl's strong-read default.
func (s *DynamoDBKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.table),
		ConsistentRead: aws.Bool(true),
		Key: map[string]types.AttributeValue{
			dynamoPKAttr: &types.AttributeValueMemberS{Value: s.moduleName},
			dynamoSKAttr: &types.AttributeValueMemberS{Value: key},
		},
		ExpressionAttributeNames: map[string]string{"#v": dynamoValueAttr},
		ProjectionExpression:     aws.String("#v"),
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb get %s/%s: %w", s.moduleName, key, err)
	}
	if len(out.Item) == 0 {
		return nil, ErrNotFound
	}
	rawAttr, ok := out.Item[dynamoValueAttr]
	if !ok {
		return nil, fmt.Errorf("dynamodb get %s/%s: missing %q attribute", s.moduleName, key, dynamoValueAttr)
	}
	bin, ok := rawAttr.(*types.AttributeValueMemberB)
	if !ok {
		return nil, fmt.Errorf("dynamodb get %s/%s: unexpected attribute type %T", s.moduleName, key, rawAttr)
	}
	return bin.Value, nil
}

// GetJSON decodes the value at key into dst.
func (s *DynamoDBKVStore) GetJSON(ctx context.Context, key string, dst any) error {
	raw, err := s.Get(ctx, key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("dynamodb get %s/%s: json decode: %w", s.moduleName, key, err)
	}
	return nil
}

// Put writes raw bytes at key, creating or overwriting.
func (s *DynamoDBKVStore) Put(ctx context.Context, key string, val []byte) error {
	if err := validateKey(key); err != nil {
		return err
	}
	// DynamoDB rejects empty Binary values. Store a single zero byte sentinel
	// transparently — Firestore allows zero-length []byte and callers may
	// rely on that. The Get path treats both as []byte; downstream JSON
	// callers will see []byte{0} where they put []byte{}, but no current
	// caller relies on storing literal empty bytes (they use PutJSON, which
	// always emits at least "null" = 4 bytes).
	if len(val) == 0 {
		val = []byte{0}
	}
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item: map[string]types.AttributeValue{
			dynamoPKAttr:        &types.AttributeValueMemberS{Value: s.moduleName},
			dynamoSKAttr:        &types.AttributeValueMemberS{Value: key},
			dynamoValueAttr:     &types.AttributeValueMemberB{Value: val},
			dynamoUpdatedAtAttr: &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().UTC().UnixNano(), 10)},
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodb put %s/%s: %w", s.moduleName, key, err)
	}
	return nil
}

// PutJSON marshals val and writes the bytes at key.
func (s *DynamoDBKVStore) PutJSON(ctx context.Context, key string, val any) error {
	raw, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("dynamodb put %s/%s: json encode: %w", s.moduleName, key, err)
	}
	return s.Put(ctx, key, raw)
}

// Delete removes the item at key. Deleting a missing key is not an error.
func (s *DynamoDBKVStore) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key: map[string]types.AttributeValue{
			dynamoPKAttr: &types.AttributeValueMemberS{Value: s.moduleName},
			dynamoSKAttr: &types.AttributeValueMemberS{Value: key},
		},
	})
	if err != nil {
		return fmt.Errorf("dynamodb delete %s/%s: %w", s.moduleName, key, err)
	}
	return nil
}

// List returns all sort keys in the partition that start with prefix. Empty
// prefix scans the entire partition (Query is still cheap because it's
// partition-bounded; no Scan).
func (s *DynamoDBKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		ProjectionExpression:   aws.String(dynamoSKAttr),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: s.moduleName},
		},
	}
	if prefix == "" {
		input.KeyConditionExpression = aws.String("pk = :pk")
	} else {
		input.KeyConditionExpression = aws.String("pk = :pk AND begins_with(sk, :prefix)")
		input.ExpressionAttributeValues[":prefix"] = &types.AttributeValueMemberS{Value: prefix}
	}

	var keys []string
	pager := dynamodb.NewQueryPaginator(s.client, input)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("dynamodb list %s prefix=%q: %w", s.moduleName, prefix, err)
		}
		for _, item := range page.Items {
			skAttr, ok := item[dynamoSKAttr]
			if !ok {
				continue
			}
			sk, ok := skAttr.(*types.AttributeValueMemberS)
			if !ok {
				continue
			}
			keys = append(keys, sk.Value)
		}
	}
	return keys, nil
}

// errIsNotFound wraps the SDK NotFound condition. DynamoDB GetItem doesn't
// return an error for missing items (returns empty Item), but other ops may
// surface ResourceNotFoundException for missing tables.
func errIsTableMissing(err error) bool {
	var rnfe *types.ResourceNotFoundException
	return errors.As(err, &rnfe)
}
