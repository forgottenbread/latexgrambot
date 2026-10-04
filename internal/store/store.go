// Package store persists settings, users, group memberships and render
// statistics in the shared MongoDB cluster.
package store

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Settings are the per-chat overrides. Zero values mean "use the default".
type Settings struct {
	Preamble string `json:"preamble,omitempty" bson:"preamble,omitempty"`
	DPI      int    `json:"dpi,omitempty" bson:"dpi,omitempty"`
}

// User is the caller information recorded on every interaction.
type User struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

// Chat is the group information recorded on membership changes.
type Chat struct {
	ID       int64
	Type     string
	Title    string
	Username string
}

// RenderStat is one render event, kept for statistics.
type RenderStat struct {
	Source        string // chat | inline
	Format        string // rich | png | pdf | inline
	ChatID        int64
	UserID        int64
	DPI           int
	ExpressionLen int
	OK            bool
	Error         string
	DurationMs    int64
}

// Store provides all persistence for the bot.
type Store struct {
	client   *mongo.Client
	settings *mongo.Collection
	users    *mongo.Collection
	groups   *mongo.Collection
	stats    *mongo.Collection
	files    *mongo.Collection
}

// New connects a Store to the given database.
func New(client *mongo.Client, database string) *Store {
	db := client.Database(database)
	return &Store{
		client:   client,
		settings: db.Collection("settings"),
		users:    db.Collection("users"),
		groups:   db.Collection("groups"),
		stats:    db.Collection("stats"),
		files:    db.Collection("files"),
	}
}

// Ping reports whether the cluster is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, readpref.PrimaryPreferred())
}

// EnsureIndexes creates the indexes the bot relies on.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	_, err := s.stats.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "ts", Value: -1}}, Options: options.Index().SetName("stats_ts")},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "ts", Value: -1}}, Options: options.Index().SetName("stats_user_ts")},
	})
	return err
}

// Get returns the settings for chatID, or the zero value.
func (s *Store) Get(chatID int64) Settings {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var settings Settings
	err := s.settings.FindOne(ctx, bson.M{"_id": chatID}).Decode(&settings)
	if err != nil {
		if err != mongo.ErrNoDocuments {
			log.Printf("store: read settings for %d: %v", chatID, err)
		}
		return Settings{}
	}
	return settings
}

// Set stores settings for chatID.
func (s *Store) Set(chatID int64, settings Settings) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.settings.UpdateOne(ctx,
		bson.M{"_id": chatID},
		bson.M{"$set": settings, "$setOnInsert": bson.M{"created_at": time.Now().UTC()}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("store: write settings for %d: %w", chatID, err)
	}
	return nil
}

// RecordUser upserts the interacting user and refreshes last_seen_at.
func (s *Store) RecordUser(user User) {
	if user.ID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	set := bson.M{"last_seen_at": now}
	for key, value := range map[string]string{
		"username": user.Username, "first_name": user.FirstName, "last_name": user.LastName,
	} {
		if strings.TrimSpace(value) != "" {
			set[key] = value
		}
	}
	_, err := s.users.UpdateOne(ctx,
		bson.M{"_id": user.ID},
		bson.M{"$set": set, "$setOnInsert": bson.M{"first_seen_at": now, "renders": int64(0)}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		log.Printf("store: record user %d: %v", user.ID, err)
	}
}

// RecordGroup upserts a chat the bot is a member of (or left).
func (s *Store) RecordGroup(chat Chat, member bool) {
	if chat.ID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	status := "left"
	if member {
		status = "member"
	}
	set := bson.M{"status": status, "updated_at": now}
	for key, value := range map[string]string{
		"type": chat.Type, "title": chat.Title, "username": chat.Username,
	} {
		if strings.TrimSpace(value) != "" {
			set[key] = value
		}
	}
	_, err := s.groups.UpdateOne(ctx,
		bson.M{"_id": chat.ID},
		bson.M{"$set": set, "$setOnInsert": bson.M{"added_at": now}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		log.Printf("store: record group %d: %v", chat.ID, err)
	}
}

// Stats are the aggregate numbers reported by the /stats command.
type Stats struct {
	Renders       int64
	ChatRenders   int64
	InlineRenders int64
	FailedRenders int64
	Renders24h    int64
	Users         int64
	Groups        int64
}

// Stats computes the aggregate counts shown by /stats. Counting is cheap
// enough for a command that is used rarely.
func (s *Store) Stats() Stats {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stats Stats
	if count, err := s.stats.CountDocuments(ctx, bson.M{}); err == nil {
		stats.Renders = count
	} else {
		log.Printf("store: count renders: %v", err)
	}
	if count, err := s.stats.CountDocuments(ctx, bson.M{"source": "chat"}); err == nil {
		stats.ChatRenders = count
	}
	if count, err := s.stats.CountDocuments(ctx, bson.M{"source": "inline"}); err == nil {
		stats.InlineRenders = count
	}
	if count, err := s.stats.CountDocuments(ctx, bson.M{"ok": false}); err == nil {
		stats.FailedRenders = count
	}
	if count, err := s.stats.CountDocuments(ctx, bson.M{"ts": bson.M{"$gte": time.Now().UTC().Add(-24 * time.Hour)}}); err == nil {
		stats.Renders24h = count
	}
	if count, err := s.users.CountDocuments(ctx, bson.M{}); err == nil {
		stats.Users = count
	}
	if count, err := s.groups.CountDocuments(ctx, bson.M{"status": "member"}); err == nil {
		stats.Groups = count
	}
	return stats
}

// RecordRender appends a render event to the statistics collection.
func (s *Store) RecordRender(stat RenderStat) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.stats.InsertOne(ctx, bson.M{
		"ts":             time.Now().UTC(),
		"source":         stat.Source,
		"format":         stat.Format,
		"chat_id":        stat.ChatID,
		"user_id":        stat.UserID,
		"dpi":            stat.DPI,
		"expression_len": stat.ExpressionLen,
		"ok":             stat.OK,
		"error":          stat.Error,
		"duration_ms":    stat.DurationMs,
	})
	if err != nil {
		log.Printf("store: record render stat: %v", err)
	}
}

// FileRefs are correctly typed Telegram file_ids captured from normal chat
// sends. They can be reused in inline results without another upload.
type FileRefs struct {
	PhotoID string
	DocID   string
}

// FileRefs returns cached Telegram references for a rendered content hash.
func (s *Store) FileRefs(hash string) FileRefs {
	if hash == "" {
		return FileRefs{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var doc struct {
		PhotoFileID string `bson:"photo_file_id"`
		DocFileID   string `bson:"document_file_id"`
	}
	err := s.files.FindOne(ctx, bson.M{"_id": hash}).Decode(&doc)
	if err != nil {
		if err != mongo.ErrNoDocuments {
			log.Printf("store: read file refs for %s: %v", hash, err)
		}
		return FileRefs{}
	}
	return FileRefs{PhotoID: doc.PhotoFileID, DocID: doc.DocFileID}
}

// SavePhotoID caches a Photo-type file_id for a content hash.
func (s *Store) SavePhotoID(hash, fileID string) {
	s.saveFileID(hash, "photo_file_id", fileID)
}

// SaveDocID caches a Document-type file_id for a content hash.
func (s *Store) SaveDocID(hash, fileID string) {
	s.saveFileID(hash, "document_file_id", fileID)
}

func (s *Store) saveFileID(hash, field, fileID string) {
	if hash == "" || fileID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	_, err := s.files.UpdateOne(ctx,
		bson.M{"_id": hash},
		bson.M{
			"$set":         bson.M{field: fileID, "updated_at": now},
			"$setOnInsert": bson.M{"created_at": now},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		log.Printf("store: save %s for %s: %v", field, hash, err)
	}
}
