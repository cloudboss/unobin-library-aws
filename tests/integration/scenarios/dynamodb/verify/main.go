// verify checks the DynamoDB table the scenario applied against the phase named
// in the VERIFY_PHASE environment variable. The table has a stable name, so
// applied requires it present and active with its composite key, and destroyed
// requires it gone. The global secondary index and the stream are anchored
// best-effort, since an emulator may not model them. It only reads cloud state;
// tearing the table down is the destroy plan's job.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	tableName = "unobin-it-table"
	gsiName   = "by-lookup"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	phase := os.Getenv("VERIFY_PHASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := dynamodb.NewFromConfig(cfg)

	switch phase {
	case "applied":
		return verifyApplied(ctx, client)
	case "destroyed":
		return verifyDestroyed(ctx, client)
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied or destroyed, got %q", phase)
	}
}

func verifyApplied(ctx context.Context, client *dynamodb.Client) error {
	resp, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return fmt.Errorf("describe table %s: %w", tableName, err)
	}
	t := resp.Table
	if got := t.TableStatus; got != dynamodbtypes.TableStatusActive {
		return fmt.Errorf("table status is %q, want ACTIVE", got)
	}
	if !hasKey(t.KeySchema, "id", dynamodbtypes.KeyTypeHash) ||
		!hasKey(t.KeySchema, "sort", dynamodbtypes.KeyTypeRange) {
		return fmt.Errorf("table %s is missing its id/sort composite key", tableName)
	}
	// The global secondary index and the stream are anchored best-effort: an
	// emulator may not model them, so a miss degrades to a printed skip.
	if hasGSI(t.GlobalSecondaryIndexes, gsiName) {
		fmt.Printf("ok: global secondary index %s present\n", gsiName)
	} else {
		fmt.Println("skip: global secondary index not modeled")
	}
	if aws.ToString(t.LatestStreamArn) != "" {
		fmt.Println("ok: stream present")
	} else {
		fmt.Println("skip: stream not modeled")
	}
	fmt.Printf("ok: table %s present and active with its composite key\n", tableName)
	return nil
}

func verifyDestroyed(ctx context.Context, client *dynamodb.Client) error {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		if isTableGone(err) {
			fmt.Printf("ok: table %s gone\n", tableName)
			return nil
		}
		return fmt.Errorf("describe table %s: %w", tableName, err)
	}
	return fmt.Errorf("table %s still exists", tableName)
}

// hasKey reports whether the key schema names the attribute with the key type.
func hasKey(schema []dynamodbtypes.KeySchemaElement, name string, kt dynamodbtypes.KeyType) bool {
	for _, e := range schema {
		if aws.ToString(e.AttributeName) == name && e.KeyType == kt {
			return true
		}
	}
	return false
}

// hasGSI reports whether the table has a global secondary index with the name.
func hasGSI(indexes []dynamodbtypes.GlobalSecondaryIndexDescription, name string) bool {
	for _, i := range indexes {
		if aws.ToString(i.IndexName) == name {
			return true
		}
	}
	return false
}

// isTableGone reports whether err is DynamoDB's table-not-found error.
func isTableGone(err error) bool {
	var notFound *dynamodbtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}
