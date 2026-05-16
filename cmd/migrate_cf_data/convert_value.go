// One-shot rewrite of the table's `value` attribute from Binary (legacy
// shape) to String (current shape). Operator-elective; needed once after the
// runtime swap from MemberB to MemberS in internal/storage/dynamodb_kv.go.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// signalContext is defined in main.go and gives every subcommand a SIGINT /
// SIGTERM-cancellable context — Ctrl-C mid-scan now propagates as a clean
// context error instead of leaving a half-converted table.
//
// (signature mirrors signal.NotifyContext for documentation purposes; no
// re-declaration here, just a pointer for future readers.)


func runConvertValueToString(args []string) error {
	fs := flag.NewFlagSet("convert-value-to-string", flag.ExitOnError)
	table := fs.String("table", "", "target DynamoDB table (required)")
	dryRun := fs.Bool("dry-run", false, "log actions but do not write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *table == "" {
		return fmt.Errorf("--table is required")
	}

	ctx, cancel := signalContext()
	defer cancel()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	client := dynamodb.NewFromConfig(cfg)

	converted, alreadyString, skipped, failed := 0, 0, 0, 0
	pager := dynamodb.NewScanPaginator(client, &dynamodb.ScanInput{TableName: aws.String(*table)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		for _, item := range page.Items {
			pk, sk, ok := itemPKSK(item)
			if !ok {
				skipped++
				continue
			}
			valAttr, ok := item["value"]
			if !ok {
				skipped++
				continue
			}
			binAttr, isBinary := valAttr.(*types.AttributeValueMemberB)
			if !isBinary {
				alreadyString++
				continue
			}
			if *dryRun {
				fmt.Printf("  DRY-RUN would convert pk=%s sk=%s len=%d\n", pk, sk, len(binAttr.Value))
				converted++
				continue
			}
			if err := putAsString(ctx, client, *table, pk, sk, binAttr.Value); err != nil {
				fmt.Fprintf(os.Stderr, "  put %s/%s: %v\n", pk, sk, err)
				failed++
				continue
			}
			converted++
		}
	}

	fmt.Printf("\nconvert-value-to-string report\n")
	fmt.Printf("  converted (B → S):       %d\n", converted)
	fmt.Printf("  already String:          %d\n", alreadyString)
	fmt.Printf("  skipped (no pk/sk/value): %d\n", skipped)
	fmt.Printf("  failed:                  %d\n", failed)
	return nil
}

func itemPKSK(item map[string]types.AttributeValue) (string, string, bool) {
	pkAttr, ok := item["pk"].(*types.AttributeValueMemberS)
	if !ok {
		return "", "", false
	}
	skAttr, ok := item["sk"].(*types.AttributeValueMemberS)
	if !ok {
		return "", "", false
	}
	return pkAttr.Value, skAttr.Value, true
}

func putAsString(ctx context.Context, client *dynamodb.Client, table, pk, sk string, val []byte) error {
	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(table),
		Item: map[string]types.AttributeValue{
			"pk":        &types.AttributeValueMemberS{Value: pk},
			"sk":        &types.AttributeValueMemberS{Value: sk},
			"value":     &types.AttributeValueMemberS{Value: string(val)},
			"updatedAt": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().UTC().UnixNano(), 10)},
		},
	})
	return err
}
