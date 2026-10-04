// latexgrambot renders LaTeX expressions into PNG, PDF and Telegram rich
// messages, entirely locally (TeX Live + poppler, no external services).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"latexgrambot/internal/bot"
	"latexgrambot/internal/config"
	"latexgrambot/internal/files"
	"latexgrambot/internal/render"
	"latexgrambot/internal/store"
	"latexgrambot/internal/telemetry"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelmongo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	// Used at image build time to generate the preamble that preloads the
	// default packages into the "latexgrambot" TeX format.
	if len(os.Args) > 1 && os.Args[1] == "-print-preamble" {
		fmt.Print(render.DefaultPreamble())
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.Setup(ctx)
	if err != nil {
		log.Fatalf("telemetry: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	mongoClient, err := mongo.Connect(ctx,
		options.Client().ApplyURI(cfg.MongoURI).SetMonitor(otelmongo.NewMonitor()),
	)
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	defer mongoClient.Disconnect(context.Background())
	storage := store.New(mongoClient, cfg.MongoDatabase)
	pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
	err = storage.Ping(pingCtx)
	pingCancel()
	if err != nil {
		log.Fatalf("MongoDB not reachable: %v", err)
	}
	if err := storage.EnsureIndexes(ctx); err != nil {
		log.Fatalf("ensure indexes: %v", err)
	}

	renderer := render.New(cfg.PdflatexBin, cfg.PdftoppmBin, cfg.RenderTimeout, cfg.MaxConcurrency, "")
	renderer.Format = cfg.LatexFormat

	var fileStore *files.Store
	if cfg.S3Enabled() {
		s3Store, storeErr := files.New(ctx, files.Config{
			Endpoint:        cfg.S3Endpoint,
			Region:          cfg.S3Region,
			Bucket:          cfg.S3Bucket,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			Prefix:          cfg.S3Prefix,
			PublicEndpoint:  cfg.S3PublicEndpoint,
			PresignTTL:      cfg.S3PresignTTL,
		})
		if storeErr != nil {
			log.Fatalf("files store: %v", storeErr)
		}
		fileStore = s3Store
		log.Printf("inline files use private bucket %s with %s presigned URLs", cfg.S3Bucket, cfg.S3PresignTTL)

		// Garbage collect inline assets once their presigned links have
		// certainly expired: Telegram downloads them at pick time, and the
		// signed URLs are dead after the presign TTL anyway.
		assetAge := 2 * cfg.S3PresignTTL
		if assetAge < time.Hour {
			assetAge = time.Hour
		}
		go func() {
			ticker := time.NewTicker(15 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					sweepCtx, cancel := context.WithTimeout(ctx, time.Minute)
					removed, sweepErr := fileStore.DeleteExpired(sweepCtx, assetAge)
					cancel()
					if sweepErr != nil {
						log.Printf("asset sweep: %v", sweepErr)
					} else if removed > 0 {
						log.Printf("asset sweep: removed %d expired objects", removed)
					}
				}
			}
		}()
	} else {
		log.Println("S3 not configured: inline PNG/PDF results disabled")
	}

	b, err := bot.New(cfg, renderer, storage, storage, storage, fileStore)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, request *http.Request) {
		checkCtx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
		defer cancel()
		if err := storage.Ping(checkCtx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "dependency": "mongodb"})
			return
		}
		if err := b.Healthy(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	srv := &http.Server{
		Addr:              cfg.HealthAddr,
		Handler:           otelhttp.NewHandler(mux, "latexgrambot-http"),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	log.Printf("latexgrambot started: dpi=%d timeout=%s concurrency=%d mongo=%s",
		cfg.RenderDPI, cfg.RenderTimeout, cfg.MaxConcurrency, cfg.MongoDatabase)
	runErr := b.Run(ctx)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
	if runErr != nil {
		log.Fatal(runErr)
	}
	log.Println("latexgrambot stopped")
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
