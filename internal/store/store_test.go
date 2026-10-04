package store

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// integrationStore connects to the cluster from MONGODB_URI (integration
// tests only; CI runs without a database and skips them).
func integrationStore(t *testing.T) *Store {
	t.Helper()
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI not set; skipping MongoDB integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("MongoDB not reachable, skipping integration test: %v", err)
	}
	return New(client, "latexgram_test_"+time.Now().Format("20060102150405"))
}

func TestSettingsRoundTrip(t *testing.T) {
	s := integrationStore(t)
	if got := s.Get(42); got != (Settings{}) {
		t.Fatalf("unexpected initial settings: %+v", got)
	}
	settings := Settings{Preamble: `\usepackage{mhchem}`, DPI: 200}
	if err := s.Set(42, settings); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get(42); got != settings {
		t.Fatalf("got %+v, want %+v", got, settings)
	}
}

func TestRecordUserAndGroup(t *testing.T) {
	s := integrationStore(t)
	userID := time.Now().UnixNano()
	s.RecordUser(User{ID: userID, Username: "someone", FirstName: "A", LastName: "B"})
	s.RecordUser(User{ID: userID})
	s.RecordGroup(Chat{ID: -1001, Type: "supergroup", Title: "Test", Username: "test"}, true)
	s.RecordGroup(Chat{ID: -1001}, false)
}
