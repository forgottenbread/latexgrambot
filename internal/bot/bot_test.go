package bot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"latexgrambot/internal/config"
	"latexgrambot/internal/render"
	"latexgrambot/internal/rich"
	"latexgrambot/internal/store"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
)

type stubRenderer struct {
	results *render.Result
	err     error
}

func (s *stubRenderer) RenderFormats(_ context.Context, _, _ string, _ int, _ render.Formats) (*render.Result, error) {
	return s.results, s.err
}

type fakeSettings struct {
	settings store.Settings
}

func (f *fakeSettings) Get(int64) store.Settings { return f.settings }
func (f *fakeSettings) Set(int64, store.Settings) error {
	return nil
}

type fakeRecorder struct{}

func (f *fakeRecorder) RecordUser(store.User)         {}
func (f *fakeRecorder) RecordGroup(store.Chat, bool)  {}
func (f *fakeRecorder) RecordRender(store.RenderStat) {}

func (f *fakeRecorder) Stats() store.Stats {
	return store.Stats{
		Renders: 42, ChatRenders: 30, InlineRenders: 12,
		FailedRenders: 3, Renders24h: 7, Users: 9, Groups: 2,
	}
}

type fakeCache struct {
	refs store.FileRefs
}

func (f *fakeCache) FileRefs(string) store.FileRefs { return f.refs }
func (f *fakeCache) SavePhotoID(_, fileID string)   { f.refs.PhotoID = fileID }
func (f *fakeCache) SaveDocID(_, fileID string)     { f.refs.DocID = fileID }

type fakeFiles struct {
	mu       sync.Mutex
	puts     map[string][]byte
	putCalls int
}

func (f *fakeFiles) Put(_ context.Context, ext, _ string, data []byte) (string, error) {
	f.mu.Lock()
	if f.puts == nil {
		f.puts = map[string][]byte{}
	}
	f.puts[ext] = data
	f.putCalls++
	f.mu.Unlock()
	return "https://s3.example/latexgrambot/content/" + uuid.NewString() + "." + ext + "?X-Amz-Signature=test", nil
}

func testBot() *Bot {
	return &Bot{
		api: &tgbotapi.BotAPI{Self: tgbotapi.User{UserName: "latexgrambot"}},
		cfg: &config.Config{MaxExpressionLen: 4000, RichTextEnabled: true, RichDefaultMath: true, RenderDPI: 600},
		ren: &stubRenderer{results: &render.Result{
			PNG: []byte("png"), JPEG: []byte("jpg"),
			JPEGWidth: 1600, JPEGHeight: 600,
			Thumbnail: []byte("thumbnail"), PDF: []byte("pdf"),
		}},
		user:          &fakeSettings{},
		rec:           &fakeRecorder{},
		files:         &fakeFiles{},
		tracer:        otel.GetTracerProvider().Tracer("test"),
		settingsCache: map[int64]store.Settings{},
		refsCache:     map[string]refsEntry{},
		urlsCache:     map[string]urlEntry{},
	}
}

func article(t *testing.T, result interface{}) tgbotapi.InlineQueryResultArticle {
	t.Helper()
	a, ok := result.(tgbotapi.InlineQueryResultArticle)
	if !ok {
		t.Fatalf("result is %T, want InlineQueryResultArticle", result)
	}
	return a
}

func TestInlineResultsUsesPresignedFiles(t *testing.T) {
	b := testBot()
	results := b.inlineResults(context.Background(), &tgbotapi.InlineQuery{Query: "$F=ma$", ChatType: "supergroup", From: &tgbotapi.User{ID: 1}})
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	photo, ok := results[0].(inlineQueryResultPhoto)
	if !ok {
		t.Fatalf("result 0 is %T, want inlineQueryResultPhoto", results[0])
	}
	if !strings.Contains(photo.PhotoURL, ".jpg?") || !strings.Contains(photo.ThumbnailURL, ".thumb.jpg?") {
		t.Errorf("photo URL/thumbnail = %q/%q", photo.PhotoURL, photo.ThumbnailURL)
	}
	if photo.PhotoURL == photo.ThumbnailURL {
		t.Error("full photo and thumbnail must use different objects")
	}
	if photo.PhotoWidth != 1600 || photo.PhotoHeight != 600 {
		t.Errorf("photo dimensions = %dx%d", photo.PhotoWidth, photo.PhotoHeight)
	}
	if photo.Title != "PNG" {
		t.Errorf("photo title = %q", photo.Title)
	}
	encoded, err := json.Marshal(photo)
	if err != nil {
		t.Fatalf("marshal photo result: %v", err)
	}
	if !strings.Contains(string(encoded), `"thumbnail_url"`) || strings.Contains(string(encoded), `"thumb_url"`) {
		t.Errorf("unexpected photo JSON: %s", encoded)
	}

	document, ok := results[1].(tgbotapi.InlineQueryResultDocument)
	if !ok {
		t.Fatalf("result 1 is %T, want InlineQueryResultDocument", results[1])
	}
	if !strings.Contains(document.URL, ".pdf?") || document.MimeType != "application/pdf" {
		t.Errorf("document URL/type = %q/%q", document.URL, document.MimeType)
	}
	if document.Title != "PDF" {
		t.Errorf("document title = %q", document.Title)
	}

	richResult := article(t, results[2])
	if richResult.Title != "Rich text" {
		t.Errorf("rich title = %q", richResult.Title)
	}
	if richResult.ReplyMarkup != nil {
		t.Error("rich result must not carry a button")
	}
	content, ok := richResult.InputMessageContent.(inputRichMessageContent)
	if !ok || content.RichMessage == nil || len(content.RichMessage.Blocks) != 1 {
		t.Fatalf("unexpected rich content: %+v", richResult.InputMessageContent)
	}
	parts, ok := content.RichMessage.Blocks[0].Text.([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("unexpected paragraph content: %+v", content.RichMessage.Blocks[0].Text)
	}
	math, ok := parts[1].(rich.Inline)
	if !ok || math.Type != "mathematical_expression" || math.Expression != "F=ma" {
		t.Fatalf("unexpected inline math node: %+v", parts[1])
	}
	files := b.files.(*fakeFiles)
	if string(files.puts["jpg"]) != "jpg" || string(files.puts["thumb.jpg"]) != "thumbnail" || string(files.puts["pdf"]) != "pdf" {
		t.Fatalf("stored files = %#v", files.puts)
	}
}

func TestInlineResultsCachesPresignedURLs(t *testing.T) {
	b := testBot()
	query := &tgbotapi.InlineQuery{Query: "$F=ma$", ChatType: "private", From: &tgbotapi.User{ID: 1}}
	first := b.inlineResults(context.Background(), query)
	second := b.inlineResults(context.Background(), query)
	files := b.files.(*fakeFiles)
	if files.putCalls != 3 {
		t.Fatalf("put calls = %d, want 3 (photo, thumbnail and PDF, cached afterwards)", files.putCalls)
	}
	firstPhoto := first[0].(inlineQueryResultPhoto)
	secondPhoto := second[0].(inlineQueryResultPhoto)
	if firstPhoto.PhotoURL != secondPhoto.PhotoURL || firstPhoto.ThumbnailURL != secondPhoto.ThumbnailURL {
		t.Fatalf("second query re-rendered: %+v vs %+v", firstPhoto, secondPhoto)
	}
}

func TestInlineResultsReusesTelegramFileIDs(t *testing.T) {
	b := testBot()
	b.cache = &fakeCache{refs: store.FileRefs{PhotoID: "photo-id", DocID: "doc-id"}}
	b.ren = &stubRenderer{err: &render.LatexError{Excerpt: "must not render"}}
	results := b.inlineResults(context.Background(), &tgbotapi.InlineQuery{Query: "$x$", ChatType: "private", From: &tgbotapi.User{ID: 1}})
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	photo, ok := results[0].(tgbotapi.InlineQueryResultCachedPhoto)
	if !ok || photo.PhotoID != "photo-id" {
		t.Fatalf("photo = %#v", results[0])
	}
	document, ok := results[1].(tgbotapi.InlineQueryResultCachedDocument)
	if !ok || document.DocumentID != "doc-id" {
		t.Fatalf("document = %#v", results[1])
	}
}

func TestInlineResultsNoFileStore(t *testing.T) {
	b := testBot()
	b.files = nil
	b.ren = &stubRenderer{err: &render.LatexError{Excerpt: "must not render"}}
	results := b.inlineResults(context.Background(), &tgbotapi.InlineQuery{Query: "$x$", ChatType: "private", From: &tgbotapi.User{ID: 1}})
	if len(results) != 1 || article(t, results[0]).Title != "Rich text" {
		t.Fatalf("results = %#v", results)
	}
}

func TestInlineResultsRichDisabled(t *testing.T) {
	b := testBot()
	b.cfg.RichTextEnabled = false
	results := b.inlineResults(context.Background(), &tgbotapi.InlineQuery{Query: "$x$", ChatType: "private", From: &tgbotapi.User{ID: 1}})
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if _, ok := results[0].(inlineQueryResultPhoto); !ok {
		t.Fatalf("result 0 is %T, want inlineQueryResultPhoto", results[0])
	}
}

func TestSaveFileIDsUsesPersistentCache(t *testing.T) {
	b := testBot()
	cache := &fakeCache{}
	b.cache = cache
	b.savePhotoID("hash", "photo-id")
	b.saveDocID("hash", "doc-id")
	if cache.refs.PhotoID != "photo-id" || cache.refs.DocID != "doc-id" {
		t.Fatalf("saved refs = %+v", cache.refs)
	}
	if got := b.refsCache["hash"].refs; got != cache.refs {
		t.Fatalf("memory refs = %+v, persistent refs = %+v", got, cache.refs)
	}
}

func TestInlineResultsRenderError(t *testing.T) {
	b := testBot()
	b.ren = &stubRenderer{err: &render.LatexError{Excerpt: "! Undefined control sequence."}}
	results := b.inlineResults(context.Background(), &tgbotapi.InlineQuery{Query: "\\bad", ChatType: "private", From: &tgbotapi.User{ID: 1}})
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if got := article(t, results[0]).Title; got != "LaTeX error" {
		t.Errorf("title = %q, want LaTeX error", got)
	}
}

func TestInlineResultsEmptyQuery(t *testing.T) {
	b := testBot()
	results := b.inlineResults(context.Background(), &tgbotapi.InlineQuery{Query: "  ", ChatType: "private", From: &tgbotapi.User{ID: 1}})
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if got := article(t, results[0]).Title; got != "Type a LaTeX expression" {
		t.Errorf("title = %q", got)
	}
}

func TestContentHashStability(t *testing.T) {
	a := contentHash("pre", "x^2", 600)
	b := contentHash("pre", "x^2", 600)
	c := contentHash("pre", "x^2", 300)
	if a != b || len(a) != 64 {
		t.Fatalf("hash must be stable: %q %q", a, b)
	}
	if a == c {
		t.Fatal("different DPI must change the hash")
	}
}

func TestStatsText(t *testing.T) {
	text := statsText(store.Stats{
		Renders: 42, ChatRenders: 30, InlineRenders: 12,
		FailedRenders: 3, Renders24h: 7, Users: 9, Groups: 2,
	})
	for _, fragment := range []string{
		"Renders: 42 (chat 30, inline 12)",
		"Failed renders: 3",
		"Renders in the last 24h: 7",
		"Users: 9",
		"Groups: 2",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("stats text missing %q:\n%s", fragment, text)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 5); got != "hello" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("hello", 3); got != "hel..." {
		t.Errorf("truncate = %q", got)
	}
}
