package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// dynamoDBLocalSetup ensures a fresh table exists in DynamoDB Local for the
// test run. Returns a client + cleanup. Tests skip if DYNAMODB_LOCAL_URL is
// unset so CI without docker still builds.
func dynamoDBLocalSetup(t *testing.T) (client *dynamodb.Client, table string, cleanup func()) {
	t.Helper()
	endpoint := os.Getenv("DYNAMODB_LOCAL_URL")
	if endpoint == "" {
		t.Skip("DYNAMODB_LOCAL_URL not set; skipping DynamoDB integration test (run `make dynamodb-local` to start the local container)")
	}
	// DynamoDB Local accepts any non-empty creds + region.
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "ap-southeast-1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := NewDynamoDBClient(ctx, endpoint)
	if err != nil {
		t.Fatalf("NewDynamoDBClient: %v", err)
	}

	table = "test-" + t.Name()
	if len(table) > 64 {
		table = table[:64]
	}

	_, err = c.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(table),
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
	})
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	cleanup = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = c.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(table)})
	}
	return c, table, cleanup
}

func TestDynamoDBKVStore_PutGetDelete(t *testing.T) {
	client, table, cleanup := dynamoDBLocalSetup(t)
	defer cleanup()

	ctx := context.Background()
	s := NewDynamoDBKVStore(client, table, "wordle")

	if err := s.Put(ctx, "user:1:state", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "user:1:state")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("Get: got %q, want %q", got, "hello")
	}

	if err := s.Delete(ctx, "user:1:state"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "user:1:state"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: got %v, want ErrNotFound", err)
	}
}

func TestDynamoDBKVStore_GetMissing(t *testing.T) {
	client, table, cleanup := dynamoDBLocalSetup(t)
	defer cleanup()

	s := NewDynamoDBKVStore(client, table, "wordle")
	_, err := s.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestDynamoDBKVStore_JSONRoundTrip(t *testing.T) {
	client, table, cleanup := dynamoDBLocalSetup(t)
	defer cleanup()

	ctx := context.Background()
	s := NewDynamoDBKVStore(client, table, "loldle")

	type state struct {
		Score int    `json:"score"`
		Name  string `json:"name"`
	}
	in := state{Score: 42, Name: "ezreal"}
	if err := s.PutJSON(ctx, "u1", in); err != nil {
		t.Fatalf("PutJSON: %v", err)
	}
	var out state
	if err := s.GetJSON(ctx, "u1", &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if out != in {
		t.Errorf("got %+v, want %+v", out, in)
	}
}

func TestDynamoDBKVStore_ListPrefix(t *testing.T) {
	client, table, cleanup := dynamoDBLocalSetup(t)
	defer cleanup()

	ctx := context.Background()
	s := NewDynamoDBKVStore(client, table, "wordle")

	for _, k := range []string{"user:1:state", "user:2:state", "config:daily", "user:1:history"} {
		if err := s.Put(ctx, k, []byte("x")); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	got, err := s.List(ctx, "user:")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := map[string]bool{"user:1:state": true, "user:2:state": true, "user:1:history": true}
	if len(got) != len(want) {
		t.Errorf("List: got %v (len=%d), want len=%d", got, len(got), len(want))
	}
	for _, k := range got {
		if !want[k] {
			t.Errorf("List: unexpected key %q", k)
		}
	}
}

func TestDynamoDBKVStore_DeleteMissingNoError(t *testing.T) {
	client, table, cleanup := dynamoDBLocalSetup(t)
	defer cleanup()

	s := NewDynamoDBKVStore(client, table, "wordle")
	if err := s.Delete(context.Background(), "never-existed"); err != nil {
		t.Errorf("Delete missing key: got %v, want nil (idempotent)", err)
	}
}
