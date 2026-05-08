package storage

import (
	"context"
	"fmt"
	"os"

	"cloud.google.com/go/firestore"
)

// NewFirestoreClient constructs a Firestore client using the project ID from
// GOOGLE_CLOUD_PROJECT. The Firestore SDK auto-detects FIRESTORE_EMULATOR_HOST
// and routes to the emulator when set, so the same constructor serves dev and
// prod.
//
// The client is goroutine-safe and meant to be reused for the lifetime of the
// process; callers should defer Close on the returned client at shutdown.
func NewFirestoreClient(ctx context.Context, projectID string) (*firestore.Client, error) {
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		return nil, fmt.Errorf("storage: GOOGLE_CLOUD_PROJECT is required for Firestore")
	}
	c, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("storage: firestore.NewClient: %w", err)
	}
	return c, nil
}
