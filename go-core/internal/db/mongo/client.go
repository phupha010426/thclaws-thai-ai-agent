// Package mongo exposes a mongo-driver client used by every service that
// touches the conversation / wiki / event_log / slip collections.
package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func New(ctx context.Context, uri string) (*mongo.Client, error) {
	opts := options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second)
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("mongo: connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo: ping: %w", err)
	}
	return client, nil
}

// DBNameFromURI extracts the database name from a connection string of the
// form mongodb://host:port/<dbname>?... so callers do not have to wire it
// twice (env URL + env DB).
func DBNameFromURI(uri string) string {
	// strip scheme
	for _, prefix := range []string{"mongodb+srv://", "mongodb://"} {
		if len(uri) >= len(prefix) && uri[:len(prefix)] == prefix {
			uri = uri[len(prefix):]
			break
		}
	}
	// trim auth + host
	for i, ch := range uri {
		if ch == '/' {
			rest := uri[i+1:]
			for j, ch := range rest {
				if ch == '?' {
					return rest[:j]
				}
			}
			return rest
		}
	}
	return ""
}
