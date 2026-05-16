// Command migrate_cf_data moves durable data from the legacy Cloudflare
// KV/D1 stack into the live AWS DynamoDB table. Operator-invoked only.
//
// Subcommands:
//
//	inventory             Read CF KV keys, apply the Phase 01 policy, print
//	                      a classification report. No writes anywhere.
//
//	kv-import             Copy migrate-action KV keys into DynamoDB.
//	                      Idempotent by default (attribute_not_exists guard).
//	                      Flags: --table, --dry-run, --overwrite.
//
//	trading-audit-dump    Stream D1 `trading_trades` rows to a JSONL file.
//	                      Audit-only; not an import input.
//	                      Flags: --out (required).
//
//	convert-value-to-string
//	                      One-shot rewrite of the table's `value` attribute
//	                      from Binary (legacy shape) to String (current
//	                      shape). Idempotent — items already stored as
//	                      String are skipped.
//	                      Flags: --table, --dry-run.
//
// Required env:
//
//	CLOUDFLARE_API_TOKEN     — read-scoped token for KV + D1
//	CLOUDFLARE_ACCOUNT_ID    — production CF account
//	CF_KV_NAMESPACE_ID       — production KV namespace
//	CF_D1_DATABASE_ID        — production D1 database (only needed for
//	                           trading-audit-dump)
//	AWS_REGION (or standard AWS SDK env) — only needed for kv-import
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/tiennm99/miti99bot/internal/migration"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]
	var err error
	switch sub {
	case "inventory":
		err = runInventory(args)
	case "kv-import":
		err = runKVImport(args)
	case "trading-audit-dump":
		err = runTradingAuditDump(args)
	case "convert-value-to-string":
		err = runConvertValueToString(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: migrate_cf_data <inventory|kv-import|trading-audit-dump|convert-value-to-string> [flags]")
}

func runInventory(args []string) error {
	fs := flag.NewFlagSet("inventory", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	kv, err := newKVClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	keys, err := kv.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}
	migrate := map[string]int{}
	skip := map[string]int{}
	for _, k := range keys {
		d := migration.Classify(k)
		if d.Action == migration.ActionMigrate {
			migrate[migration.PrefixOf(k)]++
		} else {
			skip[d.Reason]++
		}
	}
	fmt.Printf("Cloudflare KV namespace contains %d keys.\n\n", len(keys))
	fmt.Println("Migrate-action keys by prefix:")
	for p, n := range migrate {
		fmt.Printf("  %-30s %d\n", p, n)
	}
	fmt.Println("\nSkip-action keys by reason:")
	for r, n := range skip {
		fmt.Printf("  %-30s %d\n", r, n)
	}
	return nil
}

func runKVImport(args []string) error {
	fs := flag.NewFlagSet("kv-import", flag.ExitOnError)
	table := fs.String("table", "", "target DynamoDB table (required)")
	dryRun := fs.Bool("dry-run", false, "log actions but do not write")
	overwrite := fs.Bool("overwrite", false, "drop attribute_not_exists guard")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *table == "" {
		return fmt.Errorf("--table is required")
	}
	kv, err := newKVClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	var writer *migration.DynamoDBWriter
	if !*dryRun {
		cfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return fmt.Errorf("aws config: %w", err)
		}
		writer = migration.NewDynamoDBWriter(dynamodb.NewFromConfig(cfg), *table, *overwrite)
	}
	keys, err := kv.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}
	report := migration.NewReport()
	for _, k := range keys {
		d := migration.Classify(k)
		if d.Action != migration.ActionMigrate {
			report.AddSkippedPolicy(d.Reason)
			continue
		}
		val, err := kv.GetValue(ctx, k)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  get %s: %v\n", k, err)
			report.AddFailed(migration.PrefixOf(k))
			continue
		}
		if *dryRun {
			fmt.Printf("  DRY-RUN would write pk=%s sk=%s len=%d\n", d.PK, d.SK, len(val))
			report.AddImported(migration.PrefixOf(k))
			continue
		}
		switch err := writer.Put(ctx, d.PK, d.SK, val); err {
		case nil:
			report.AddImported(migration.PrefixOf(k))
		case migration.ErrItemExists:
			report.AddSkippedExisting(migration.PrefixOf(k))
		default:
			fmt.Fprintf(os.Stderr, "  put %s/%s: %v\n", d.PK, d.SK, err)
			report.AddFailed(migration.PrefixOf(k))
		}
	}
	report.Format(os.Stdout)
	return nil
}

func runTradingAuditDump(args []string) error {
	fs := flag.NewFlagSet("trading-audit-dump", flag.ExitOnError)
	out := fs.String("out", "", "output JSONL file path (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	d1, err := newD1Client()
	if err != nil {
		return err
	}
	rows, err := d1.Query(context.Background(),
		"SELECT id, user_id, symbol, side, qty, price_vnd, ts FROM trading_trades ORDER BY id", nil)
	if err != nil {
		return fmt.Errorf("d1 query: %w", err)
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	fmt.Printf("Wrote %d rows to %s\n", len(rows), *out)
	return nil
}

func newKVClient() (*migration.CloudflareKVClient, error) {
	token, account, ns := os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("CLOUDFLARE_ACCOUNT_ID"), os.Getenv("CF_KV_NAMESPACE_ID")
	if token == "" || account == "" || ns == "" {
		return nil, fmt.Errorf("set CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, CF_KV_NAMESPACE_ID")
	}
	return migration.NewCloudflareKVClient(account, ns, token), nil
}

func newD1Client() (*migration.CloudflareD1Client, error) {
	token, account, db := os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("CLOUDFLARE_ACCOUNT_ID"), os.Getenv("CF_D1_DATABASE_ID")
	if token == "" || account == "" || db == "" {
		return nil, fmt.Errorf("set CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, CF_D1_DATABASE_ID")
	}
	return migration.NewCloudflareD1Client(account, db, token), nil
}
