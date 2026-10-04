// Package bot implements the Telegram-facing behavior: messages render LaTeX
// into a PNG picture, a PDF document or a Telegram rich message, inline
// queries offer the same three formats, and commands manage per-user
// settings.
package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"latexgrambot/internal/config"
	"latexgrambot/internal/render"
	"latexgrambot/internal/rich"
	"latexgrambot/internal/store"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	pollTimeout  = 30 // seconds, Telegram long-poll
	pollStale    = 3 * time.Minute
	minDPI       = 72
	maxDPI       = 1200
	displayLimit = 3800 // long texts are truncated before being echoed back

	// Included in content hashes so renderer/template changes never reuse
	// stale Telegram file_ids or S3 objects from an older deployment.
	renderCacheVersion = "presigned-v4-rich-environments"
)

// outputFormat is one of the three generation formats.
type outputFormat int

const (
	formatImage outputFormat = iota
	formatPDF
	formatRich
)

// chatAction returns the Telegram chat action shown while the format is
// being produced.
func (f outputFormat) chatAction() string {
	switch f {
	case formatPDF:
		return tgbotapi.ChatUploadDocument
	case formatRich:
		return tgbotapi.ChatTyping
	default:
		return tgbotapi.ChatUploadPhoto
	}
}

// renderer renders LaTeX into PNG and PDF bytes.
type renderer interface {
	RenderFormats(ctx context.Context, preamble, expression string, dpi int, formats render.Formats) (*render.Result, error)
}

// settingsProvider holds per-chat settings.
type settingsProvider interface {
	Get(chatID int64) store.Settings
	Set(chatID int64, settings store.Settings) error
}

// recorder persists users, group memberships and render statistics, and
// reports the aggregates behind /stats.
type recorder interface {
	RecordUser(user store.User)
	RecordGroup(chat store.Chat, member bool)
	RecordRender(stat store.RenderStat)
	Stats() store.Stats
}

// fileCache stores correctly typed Telegram file_ids captured from ordinary
// chat sends. Inline answers can reuse those IDs without rendering or
// uploading anything.
type fileCache interface {
	FileRefs(hash string) store.FileRefs
	SavePhotoID(hash, fileID string)
	SaveDocID(hash, fileID string)
}

// FileStore stores rendered outputs and returns presigned URLs that Telegram
// can fetch without the bot sending a temporary message to any chat.
type FileStore interface {
	Put(ctx context.Context, ext, contentType string, data []byte) (string, error)
}

// Bot bridges Telegram and the local LaTeX renderer.
type Bot struct {
	api    *tgbotapi.BotAPI
	cfg    *config.Config
	ren    renderer
	user   settingsProvider
	rec    recorder
	cache  fileCache
	files  FileStore
	tracer trace.Tracer

	lastPoll atomic.Int64 // unix time of the last successful getUpdates

	settingsMu    sync.Mutex
	settingsCache map[int64]store.Settings
	refsMu        sync.Mutex
	refsCache     map[string]refsEntry
	urlsMu        sync.Mutex
	urlsCache     map[string]urlEntry
}

const refsCacheTTL = 15 * time.Minute

type refsEntry struct {
	refs store.FileRefs
	at   time.Time
}

// urlEntry caches the presigned URLs of a recent render so repeated
// identical inline queries skip rendering and uploading again.
type urlEntry struct {
	photoURL     string
	thumbnailURL string
	photoWidth   int
	photoHeight  int
	pdfURL       string
	expires      time.Time
}

// New authenticates the bot and instruments its HTTP client.
func New(cfg *config.Config, renderer renderer, users settingsProvider, rec recorder, cache fileCache, files FileStore) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return nil, fmt.Errorf("telegram bot init: %w", err)
	}
	api.Client = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	log.Printf("Authorised as @%s", api.Self.UserName)
	return &Bot{
		api:           api,
		cfg:           cfg,
		ren:           renderer,
		user:          users,
		rec:           rec,
		cache:         cache,
		files:         files,
		tracer:        otel.Tracer("latexgrambot/bot"),
		settingsCache: map[int64]store.Settings{},
		refsCache:     map[string]refsEntry{},
		urlsCache:     map[string]urlEntry{},
	}, nil
}

// Healthy reports whether Telegram polling has been failing for a while
// (e.g. a 409 conflict because another process polls the same token).
func (b *Bot) Healthy() error {
	last := b.lastPoll.Load()
	if last == 0 {
		return nil // not started yet
	}
	if age := time.Since(time.Unix(last, 0)); age > pollStale {
		return fmt.Errorf("no successful Telegram poll for %s", age.Round(time.Second))
	}
	return nil
}

// Run polls Telegram until ctx is cancelled. Updates are processed by a small
// worker pool so slow chat renders do not stall inline queries.
func (b *Bot) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workers := b.cfg.MaxConcurrency + 2
	updatesCh := make(chan tgbotapi.Update, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for upd := range updatesCh {
				b.handleUpdate(upd)
			}
		}()
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = pollTimeout
	u.AllowedUpdates = []string{"message", "inline_query", "my_chat_member"}
	b.lastPoll.Store(time.Now().Unix())
	log.Println("Listening for Telegram updates")
	for ctx.Err() == nil {
		updates, err := b.api.GetUpdates(u)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("telegram getUpdates failed: %v", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}
		// A successful response (even an empty one) proves the polling
		// loop is healthy; long-poll timeouts with no updates are normal.
		b.lastPoll.Store(time.Now().Unix())
		if len(updates) > 0 {
			u.Offset = updates[len(updates)-1].UpdateID + 1
			for _, upd := range updates {
				updatesCh <- upd
			}
		}
	}
	close(updatesCh)
	wg.Wait()
	return nil
}

func (b *Bot) handleUpdate(upd tgbotapi.Update) {
	switch {
	case upd.Message != nil:
		b.handleMessage(upd.Message)
	case upd.InlineQuery != nil:
		b.handleInlineQuery(upd.InlineQuery)
	case upd.MyChatMember != nil:
		b.handleMyChatMember(upd.MyChatMember)
	}
}

// handleMyChatMember records groups the bot is added to or removed from.
func (b *Bot) handleMyChatMember(update *tgbotapi.ChatMemberUpdated) {
	status := update.NewChatMember.Status
	member := status == "member" || status == "administrator" || status == "creator" || status == "restricted"
	go b.rec.RecordGroup(store.Chat{
		ID:       update.Chat.ID,
		Type:     update.Chat.Type,
		Title:    update.Chat.Title,
		Username: update.Chat.UserName,
	}, member)
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	ctx, span := b.tracer.Start(context.Background(), "handle_message",
		trace.WithAttributes(attribute.Int64("chat.id", msg.Chat.ID)))
	defer span.End()

	if msg.From != nil {
		go b.rec.RecordUser(store.User{
			ID:        msg.From.ID,
			Username:  msg.From.UserName,
			FirstName: msg.From.FirstName,
			LastName:  msg.From.LastName,
		})
	}
	var userID int64
	if msg.From != nil {
		userID = msg.From.ID
	}
	if msg.IsCommand() {
		b.handleCommand(msg, userID)
		return
	}
	if strings.TrimSpace(msg.Text) == "" {
		return
	}
	b.renderExpression(ctx, msg.Chat.ID, userID, msg.Text, formatRich)
}

// handleCommand routes the known commands. Unknown commands (e.g. \frac) are
// rendered as ordinary LaTeX and answered with the default rich text format.
func (b *Bot) handleCommand(msg *tgbotapi.Message, userID int64) {
	switch msg.Command() {
	case "start":
		b.sendText(msg.Chat.ID, welcomeText(b.api.Self.UserName))
	case "help":
		b.sendText(msg.Chat.ID, helpText(b.api.Self.UserName))
	case "preamble":
		b.showPreamble(msg)
	case "setpreamble":
		b.setPreamble(msg)
	case "resetpreamble":
		b.resetPreamble(msg)
	case "setdpi":
		b.setDPI(msg)
	case "settings":
		b.showSettings(msg)
	case "stats":
		b.sendText(msg.Chat.ID, statsText(b.rec.Stats()))
	case "pdf", "image", "rich":
		b.renderCommand(msg, userID)
	default:
		b.renderExpression(context.Background(), msg.Chat.ID, userID, msg.Text, formatRich)
	}
}

// renderCommand renders the arguments of a /pdf, /image or /rich command and
// replies with exactly that format.
func (b *Bot) renderCommand(msg *tgbotapi.Message, userID int64) {
	format := formatImage
	switch msg.Command() {
	case "pdf":
		format = formatPDF
	case "rich":
		format = formatRich
	}
	expression := strings.TrimSpace(msg.CommandArguments())
	if expression == "" {
		b.sendText(msg.Chat.ID, fmt.Sprintf("Usage: /%s <LaTeX code>", msg.Command()))
		return
	}
	b.renderExpression(context.Background(), msg.Chat.ID, userID, expression, format)
}

// renderExpression renders the expression and sends only the requested
// format back to the chat, recording the outcome in the statistics.
func (b *Bot) renderExpression(ctx context.Context, chatID, userID int64, expression string, format outputFormat) {
	started := time.Now()
	if len(expression) > b.cfg.MaxExpressionLen {
		b.sendText(chatID, fmt.Sprintf("Expression is too long (max %d characters).", b.cfg.MaxExpressionLen))
		return
	}
	if format == formatRich {
		b.sendRichText(chatID, expression)
		go b.rec.RecordRender(store.RenderStat{
			Source: "chat", Format: "rich", ChatID: chatID, UserID: userID,
			ExpressionLen: len(expression), OK: true, DurationMs: time.Since(started).Milliseconds(),
		})
		return
	}
	if _, err := b.api.Send(tgbotapi.NewChatAction(chatID, format.chatAction())); err != nil {
		log.Printf("send chat action to %d: %v", chatID, err)
	}

	settings := b.getSettings(chatID)
	dpi := b.dpiFor(settings)
	hash := contentHash(settings.Preamble, expression, dpi)
	formats := render.FormatPNG
	if format == formatPDF {
		formats = render.FormatPDF
	}
	renderCtx, cancel := context.WithTimeout(ctx, b.cfg.RenderTimeout)
	defer cancel()
	result, err := b.ren.RenderFormats(renderCtx, settings.Preamble, expression, dpi, formats)

	formatName := "png"
	if format == formatPDF {
		formatName = "pdf"
	}
	stat := store.RenderStat{
		Source: "chat", Format: formatName, ChatID: chatID, UserID: userID,
		DPI: dpi, ExpressionLen: len(expression),
		OK: err == nil, DurationMs: time.Since(started).Milliseconds(),
	}
	if err != nil {
		stat.Error = err.Error()
		b.sendRenderError(chatID, err)
		go b.rec.RecordRender(stat)
		return
	}
	go b.rec.RecordRender(stat)

	switch format {
	case formatPDF:
		if fileID := b.sendDocument(chatID, result.PDF); fileID != "" && b.cache != nil {
			b.saveDocID(hash, fileID)
		}
	default:
		if fileID := b.sendPhoto(chatID, result.PNG); fileID != "" && b.cache != nil {
			b.savePhotoID(hash, fileID)
		}
	}
}

// sendRichText replies with the rich-message rendering of the expression: a
// single native math block, no source echo. Failures are non-fatal. The
// pinned library predates the rich message API, so the request is built as
// raw params.
func (b *Bot) sendRichText(chatID int64, expression string) {
	if !b.cfg.RichTextEnabled {
		return
	}
	if _, err := b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)); err != nil {
		log.Printf("send chat action to %d: %v", chatID, err)
	}
	params := tgbotapi.Params{}
	params.AddNonZero64("chat_id", chatID)
	if err := params.AddInterface("rich_message", rich.Document(expression, b.cfg.RichDefaultMath)); err != nil {
		log.Printf("encode rich message: %v", err)
		return
	}
	if _, err := b.api.MakeRequest("sendRichMessage", params); err != nil {
		log.Printf("sendRichMessage to %d: %v", chatID, err)
	}
}

func (b *Bot) handleInlineQuery(query *tgbotapi.InlineQuery) {
	ctx, span := b.tracer.Start(context.Background(), "answer_inline_query",
		trace.WithAttributes(attribute.String("chat.type", query.ChatType)))
	defer span.End()

	go b.rec.RecordUser(store.User{
		ID:        query.From.ID,
		Username:  query.From.UserName,
		FirstName: query.From.FirstName,
		LastName:  query.From.LastName,
	})

	answer := tgbotapi.InlineConfig{
		InlineQueryID: query.ID,
		Results:       b.inlineResults(ctx, query),
		IsPersonal:    true,
	}
	if _, err := b.api.Request(answer); err != nil {
		log.Printf("answer inline query %s: %v", query.ID, err)
	}
}

// getSettings serves settings from the write-through cache; every write
// goes through saveSettings, so the cache stays consistent.
func (b *Bot) getSettings(chatID int64) store.Settings {
	b.settingsMu.Lock()
	settings, ok := b.settingsCache[chatID]
	b.settingsMu.Unlock()
	if ok {
		return settings
	}
	settings = b.user.Get(chatID)
	b.settingsMu.Lock()
	if b.settingsCache == nil {
		b.settingsCache = map[int64]store.Settings{}
	}
	b.settingsCache[chatID] = settings
	b.settingsMu.Unlock()
	return settings
}

func (b *Bot) saveSettings(chatID int64, settings store.Settings) error {
	if err := b.user.Set(chatID, settings); err != nil {
		return err
	}
	b.settingsMu.Lock()
	if b.settingsCache == nil {
		b.settingsCache = map[int64]store.Settings{}
	}
	b.settingsCache[chatID] = settings
	b.settingsMu.Unlock()
	return nil
}

// fileRefs serves Telegram file_ids from a short-lived cache to keep the
// inline hot path free of database round-trips.
func (b *Bot) fileRefs(hash string) store.FileRefs {
	b.refsMu.Lock()
	entry, ok := b.refsCache[hash]
	b.refsMu.Unlock()
	if ok && time.Since(entry.at) < refsCacheTTL {
		return entry.refs
	}
	refs := store.FileRefs{}
	if b.cache != nil {
		refs = b.cache.FileRefs(hash)
	}
	b.refsMu.Lock()
	if b.refsCache == nil {
		b.refsCache = map[string]refsEntry{}
	}
	b.refsCache[hash] = refsEntry{refs: refs, at: time.Now()}
	b.refsMu.Unlock()
	return refs
}

func (b *Bot) savePhotoID(hash, fileID string) {
	if b.cache == nil {
		return
	}
	b.cache.SavePhotoID(hash, fileID)
	b.refsMu.Lock()
	entry := b.refsCache[hash]
	entry.refs.PhotoID = fileID
	entry.at = time.Now()
	b.refsCache[hash] = entry
	b.refsMu.Unlock()
}

func (b *Bot) saveDocID(hash, fileID string) {
	if b.cache == nil {
		return
	}
	b.cache.SaveDocID(hash, fileID)
	b.refsMu.Lock()
	entry := b.refsCache[hash]
	entry.refs.DocID = fileID
	entry.at = time.Now()
	b.refsCache[hash] = entry
	b.refsMu.Unlock()
}

// cachedURLs returns the presigned URLs and photo geometry of a recent render
// of the same content, so repeated identical queries skip render and upload.
func (b *Bot) cachedURLs(hash string) (urlEntry, bool) {
	b.urlsMu.Lock()
	defer b.urlsMu.Unlock()
	entry, found := b.urlsCache[hash]
	if !found || time.Now().After(entry.expires) {
		return urlEntry{}, false
	}
	return entry, true
}

func (b *Bot) cacheURLs(hash string, entry urlEntry) {
	ttl := b.cfg.S3PresignTTL - 10*time.Minute
	if ttl < 5*time.Minute {
		ttl = 5 * time.Minute
	}
	entry.expires = time.Now().Add(ttl)
	b.urlsMu.Lock()
	if b.urlsCache == nil {
		b.urlsCache = map[string]urlEntry{}
	}
	b.urlsCache[hash] = entry
	b.urlsMu.Unlock()
}

// inlineResults stores rendered files in private object storage and returns
// URL-based results with short-lived signatures. Telegram fetches the selected
// file directly; generating an inline answer never sends or deletes a message
// in the user's chat.
func (b *Bot) inlineResults(ctx context.Context, query *tgbotapi.InlineQuery) []interface{} {
	raw := strings.TrimSpace(query.Query)
	if raw == "" {
		const example = `\int_0^\infty e^{-x^2}\,dx`
		return []interface{}{b.article(
			"empty", "Type a LaTeX expression", example,
			"e.g. "+example,
		)}
	}
	if len(raw) > b.cfg.MaxExpressionLen {
		return []interface{}{b.article(
			"long", "Expression too long",
			fmt.Sprintf("The expression is limited to %d characters.", b.cfg.MaxExpressionLen),
			fmt.Sprintf("The expression is limited to %d characters.", b.cfg.MaxExpressionLen),
		)}
	}
	expr := raw

	settings := b.getSettings(query.From.ID)
	dpi := b.dpiFor(settings)
	hash := contentHash(settings.Preamble, expr, dpi)
	id := hash[:40]

	refs := b.fileRefs(hash)
	needPhoto := refs.PhotoID == ""
	needDoc := refs.DocID == ""
	var urls urlEntry
	if needPhoto || needDoc {
		if cached, ok := b.cachedURLs(hash); ok {
			urls = cached
			if cached.photoURL != "" && cached.thumbnailURL != "" {
				needPhoto = false
			}
			if cached.pdfURL != "" {
				needDoc = false
			}
		}
	}
	if b.files != nil && (needPhoto || needDoc) {
		renderCtx, cancel := context.WithTimeout(ctx, b.cfg.RenderTimeout)
		defer cancel()
		started := time.Now()
		var formats render.Formats
		if needPhoto {
			formats |= render.FormatJPEG | render.FormatThumbnail
		}
		if needDoc {
			formats |= render.FormatPDF
		}
		result, err := b.ren.RenderFormats(renderCtx, settings.Preamble, expr, dpi, formats)
		stat := store.RenderStat{
			Source: "inline", Format: "inline", UserID: query.From.ID,
			DPI: dpi, ExpressionLen: len(expr), OK: err == nil,
			DurationMs: time.Since(started).Milliseconds(),
		}
		if err != nil {
			stat.Error = err.Error()
			go b.rec.RecordRender(stat)
			return []interface{}{b.errorArticle(err)}
		}
		go b.rec.RecordRender(stat)

		// Upload the requested formats concurrently: each Put is an object
		// store round-trip. A photo is only offered if both its full image and
		// its dedicated thumbnail were stored successfully.
		var wg sync.WaitGroup
		var photoPutURL, thumbnailPutURL, pdfPutURL string
		if needPhoto {
			wg.Add(2)
			go func() {
				defer wg.Done()
				storedURL, putErr := b.files.Put(ctx, "jpg", "image/jpeg", result.JPEG)
				if putErr != nil {
					log.Printf("store inline picture: %v", putErr)
					return
				}
				photoPutURL = storedURL
			}()
			go func() {
				defer wg.Done()
				storedURL, putErr := b.files.Put(ctx, "thumb.jpg", "image/jpeg", result.Thumbnail)
				if putErr != nil {
					log.Printf("store inline thumbnail: %v", putErr)
					return
				}
				thumbnailPutURL = storedURL
			}()
		}
		if needDoc {
			wg.Add(1)
			go func() {
				defer wg.Done()
				storedURL, putErr := b.files.Put(ctx, "pdf", "application/pdf", result.PDF)
				if putErr != nil {
					log.Printf("store inline document: %v", putErr)
					return
				}
				pdfPutURL = storedURL
			}()
		}
		wg.Wait()
		if photoPutURL != "" && thumbnailPutURL != "" {
			urls.photoURL = photoPutURL
			urls.thumbnailURL = thumbnailPutURL
			urls.photoWidth = result.JPEGWidth
			urls.photoHeight = result.JPEGHeight
		}
		if pdfPutURL != "" {
			urls.pdfURL = pdfPutURL
		}
		if urls.photoURL != "" || urls.pdfURL != "" {
			b.cacheURLs(hash, urls)
		}
	}

	results := []interface{}{}
	if refs.PhotoID != "" {
		photo := tgbotapi.NewInlineQueryResultCachedPhoto("photo-"+id, refs.PhotoID)
		photo.Title = "PNG"
		results = append(results, photo)
	} else if urls.photoURL != "" && urls.thumbnailURL != "" {
		results = append(results, inlineQueryResultPhoto{
			Type:         "photo",
			ID:           "photo-" + id,
			PhotoURL:     urls.photoURL,
			ThumbnailURL: urls.thumbnailURL,
			PhotoWidth:   urls.photoWidth,
			PhotoHeight:  urls.photoHeight,
			Title:        "PNG",
		})
	}
	if refs.DocID != "" {
		document := tgbotapi.NewInlineQueryResultCachedDocument("doc-"+id, refs.DocID, "PDF")
		document.Title = "PDF"
		results = append(results, document)
	} else if urls.pdfURL != "" {
		document := tgbotapi.NewInlineQueryResultDocument("doc-"+id, urls.pdfURL, "formula.pdf", "application/pdf")
		document.Title = "PDF"
		results = append(results, document)
	}
	if b.cfg.RichTextEnabled {
		results = append(results, b.richTextResult("formula-rich-"+id, raw))
	}
	if len(results) == 0 {
		return []interface{}{b.article(
			"nofiles", "Rendering unavailable",
			"No output format is available for inline answers.",
			"No output format is available for inline answers.",
		)}
	}
	return results
}

// The formula renders inline with a one-space gap from the bot mention.
func (b *Bot) richTextResult(id, expression string) interface{} {
	result := tgbotapi.NewInlineQueryResultArticle(id, "Rich text", "")
	result.Description = truncate(expression, 64)
	result.InputMessageContent = inputRichMessageContent{RichMessage: rich.InlineDocument(expression, b.cfg.RichDefaultMath)}
	return result
}

// inputRichMessageContent implements the Bot API 10.x InputRichMessageContent
// used as inline input_message_content.
type inputRichMessageContent struct {
	RichMessage *rich.Message `json:"rich_message"`
}

// inlineQueryResultPhoto follows the current Bot API field names. The pinned
// Telegram library predates thumbnail_url and still serializes thumb_url.
type inlineQueryResultPhoto struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	PhotoURL     string `json:"photo_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	PhotoWidth   int    `json:"photo_width,omitempty"`
	PhotoHeight  int    `json:"photo_height,omitempty"`
	Title        string `json:"title,omitempty"`
}

// sendPhoto sends a rendered picture to the chat and returns its reusable
// Telegram file_id.
func (b *Bot) sendPhoto(chatID int64, png []byte) string {
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{Name: "formula.png", Bytes: png})
	msg, err := b.api.Send(photo)
	if err != nil {
		log.Printf("send png to %d: %v", chatID, err)
		return ""
	}
	var best tgbotapi.PhotoSize
	for _, size := range msg.Photo {
		if size.FileSize > best.FileSize {
			best = size
		}
	}
	return best.FileID
}

// sendDocument sends the rendered PDF to the chat and returns its reusable
// Telegram file_id.
func (b *Bot) sendDocument(chatID int64, pdf []byte) string {
	document := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: "formula.pdf", Bytes: pdf})
	msg, err := b.api.Send(document)
	if err != nil {
		log.Printf("send pdf to %d: %v", chatID, err)
		return ""
	}
	if msg.Document == nil {
		return ""
	}
	return msg.Document.FileID
}

// article builds a plain article result; tapping it sends text back to the
// chat. description, when non-empty, is shown under the title in the query
// menu.
func (b *Bot) article(id, title, text, description string) interface{} {
	result := tgbotapi.NewInlineQueryResultArticle(id, title, text)
	if description != "" {
		result.Description = truncate(description, 200)
	}
	return result
}

func (b *Bot) errorArticle(err error) interface{} {
	var latexErr *render.LatexError
	switch {
	case errors.As(err, &latexErr):
		return b.article("latex-error", "LaTeX error", latexErr.Excerpt, latexErr.Excerpt)
	case errors.Is(err, context.DeadlineExceeded):
		return b.article("timeout", "Rendering timed out",
			"Rendering took too long. Simplify the expression or check the preamble.",
			"Rendering took too long. Simplify the expression or check the preamble.")
	default:
		return b.article("failed", "Rendering failed", "Something went wrong while rendering.",
			"Something went wrong while rendering.")
	}
}

func (b *Bot) sendRenderError(chatID int64, err error) {
	var latexErr *render.LatexError
	switch {
	case errors.As(err, &latexErr):
		b.sendText(chatID, "LaTeX error:\n\n"+latexErr.Excerpt)
	case errors.Is(err, context.DeadlineExceeded):
		b.sendText(chatID, "Rendering timed out. Simplify the expression or check the preamble.")
	default:
		b.sendText(chatID, "Rendering failed: "+err.Error())
	}
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("reply to %d: %v", chatID, err)
	}
}

func (b *Bot) showPreamble(msg *tgbotapi.Message) {
	settings := b.getSettings(msg.Chat.ID)
	if settings.Preamble == "" {
		b.sendText(msg.Chat.ID, "You are using the default preamble. Set your own with /setpreamble.")
		return
	}
	b.sendText(msg.Chat.ID, "Your preamble additions:\n\n"+truncate(settings.Preamble, displayLimit))
}

func (b *Bot) setPreamble(msg *tgbotapi.Message) {
	code := strings.TrimSpace(msg.CommandArguments())
	if code == "" {
		b.sendText(msg.Chat.ID, "Usage: /setpreamble <LaTeX packages and definitions>\n"+
			"Your additions are loaded after the default package set.")
		return
	}
	if len(code) > b.cfg.MaxPreambleLen {
		b.sendText(msg.Chat.ID, fmt.Sprintf("Preamble is too long (max %d characters).", b.cfg.MaxPreambleLen))
		return
	}
	settings := b.getSettings(msg.Chat.ID)
	settings.Preamble = code
	if err := b.saveSettings(msg.Chat.ID, settings); err != nil {
		log.Printf("store preamble for %d: %v", msg.Chat.ID, err)
		b.sendText(msg.Chat.ID, "Could not save the preamble, sorry.")
		return
	}
	b.sendText(msg.Chat.ID, "Preamble additions updated. Use /resetpreamble to remove them.")
}

func (b *Bot) resetPreamble(msg *tgbotapi.Message) {
	settings := b.getSettings(msg.Chat.ID)
	settings.Preamble = ""
	if err := b.saveSettings(msg.Chat.ID, settings); err != nil {
		log.Printf("reset preamble for %d: %v", msg.Chat.ID, err)
		b.sendText(msg.Chat.ID, "Could not reset the preamble, sorry.")
		return
	}
	b.sendText(msg.Chat.ID, "Preamble reset to the default.")
}

func (b *Bot) setDPI(msg *tgbotapi.Message) {
	value, err := strconv.Atoi(strings.TrimSpace(msg.CommandArguments()))
	if err != nil || value < minDPI || value > maxDPI {
		b.sendText(msg.Chat.ID, fmt.Sprintf("Usage: /setdpi <%d-%d> (default %d).", minDPI, maxDPI, b.cfg.RenderDPI))
		return
	}
	settings := b.getSettings(msg.Chat.ID)
	settings.DPI = value
	if err := b.saveSettings(msg.Chat.ID, settings); err != nil {
		log.Printf("store dpi for %d: %v", msg.Chat.ID, err)
		b.sendText(msg.Chat.ID, "Could not save the resolution, sorry.")
		return
	}
	b.sendText(msg.Chat.ID, fmt.Sprintf("Resolution set to %d DPI. Use /setdpi %d to restore the default.", value, b.cfg.RenderDPI))
}

func (b *Bot) showSettings(msg *tgbotapi.Message) {
	settings := b.getSettings(msg.Chat.ID)
	dpi := b.dpiFor(settings)
	var text strings.Builder
	fmt.Fprintf(&text, "Resolution: %d DPI\n", dpi)
	if settings.Preamble == "" {
		text.WriteString("Preamble: default")
	} else {
		text.WriteString("Preamble additions:\n\n")
		text.WriteString(truncate(settings.Preamble, displayLimit))
	}
	b.sendText(msg.Chat.ID, text.String())
}

func (b *Bot) dpiFor(settings store.Settings) int {
	if settings.DPI > 0 {
		return settings.DPI
	}
	return b.cfg.RenderDPI
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

// contentHash identifies the rendered output of an expression with a given
// preamble and DPI. It also names the stored files.
func contentHash(preamble, expression string, dpi int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", renderCacheVersion, preamble, expression, dpi)))
	return hex.EncodeToString(sum[:])
}

// statsText renders the public /stats summary.
func statsText(stats store.Stats) string {
	return fmt.Sprintf(
		"latexgrambot stats\n\n"+
			"Renders: %d (chat %d, inline %d)\n"+
			"Failed renders: %d\n"+
			"Renders in the last 24h: %d\n"+
			"Users: %d\n"+
			"Groups: %d",
		stats.Renders, stats.ChatRenders, stats.InlineRenders,
		stats.FailedRenders, stats.Renders24h, stats.Users, stats.Groups)
}

func welcomeText(username string) string {
	return fmt.Sprintf(
		"Hi! I render LaTeX into rich text messages.\n\n"+
			"Send me any LaTeX expression in this chat and I will reply with a rich text message. "+
			"Use /image or /pdf to get files instead. "+
			"You can also use me inline: in any chat, type @%s followed by an expression.\n\n"+
			"See /help for the available commands.", username)
}

func helpText(username string) string {
	return fmt.Sprintf(
		"Usage\n"+
			"- Chat mode: send a message containing LaTeX code (e.g. \\int_0^\\infty e^{-x^2}\\,dx) "+
			"and I reply with a rich text message.\n"+
			"- /rich <code> — reply with the rich text message (default)\n"+
			"- /image <code> — reply with the PNG picture\n"+
			"- /pdf <code> — reply with the PDF document\n"+
			"- Inline mode: in any chat, type @%s <expression> and pick the rich text, picture or PDF result.\n"+
			"- The document class is fixed to the standalone preview layout; "+
			"your code goes between \\begin{document} and \\end{document}.\n\n"+
			"Commands\n"+
			"/preamble — show your current preamble\n"+
			"/setpreamble <code> — replace the packages block with your own preamble\n"+
			"/resetpreamble — restore the default preamble\n"+
			"/setdpi <72-1200> — set the picture resolution (default 600)\n"+
			"/settings — show your current settings\n"+
			"/stats — show bot usage statistics\n"+
			"/help — this message", username)
}
