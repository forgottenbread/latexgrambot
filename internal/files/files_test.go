package files

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPutUploadsUniqueUUIDObjectsWithPresignedURLs(t *testing.T) {
	var puts atomic.Int32
	keys := make(chan string, 4)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status := http.StatusOK
		switch request.Method {
		case http.MethodPut:
			puts.Add(1)
			keys <- request.URL.Path
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})}

	store, err := New(context.Background(), Config{
		Endpoint:        "https://s3.internal",
		PublicEndpoint:  "https://s3.example",
		Region:          "us-east-1",
		Bucket:          "latexgrambot",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Prefix:          "content",
		PresignTTL:      time.Hour,
		HTTPClient:      client,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	seen := map[string]bool{}
	for range 2 {
		signed, err := store.Put(context.Background(), "jpg", "image/jpeg", []byte("image"))
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		parsed, err := url.Parse(signed)
		if err != nil {
			t.Fatalf("parse signed URL: %v", err)
		}
		base := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/latexgrambot/content/"), ".jpg")
		if _, err := uuid.Parse(base); err != nil || uuid.MustParse(base).Version() != 4 {
			t.Errorf("object name %q is not a UUIDv4", base)
		}
		if seen[parsed.Path] {
			t.Errorf("object name %q was reused", parsed.Path)
		}
		seen[parsed.Path] = true
		if parsed.Query().Get("X-Amz-Signature") == "" || parsed.Query().Get("X-Amz-Expires") != "3600" {
			t.Errorf("URL is not signed for one hour: %s", signed)
		}
	}
	if got := puts.Load(); got != 2 {
		t.Fatalf("PUT requests = %d, want 2 (one object per request)", got)
	}
}

func TestDeleteExpired(t *testing.T) {
	var deleted []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		header := make(http.Header)
		body := ""
		status := http.StatusOK
		switch request.Method {
		case http.MethodGet: // ListObjectsV2
			header.Set("Content-Type", "application/xml")
			body = `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>latexgrambot</Name><Prefix>content/</Prefix><KeyCount>2</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>
<Contents><Key>content/old.jpg</Key><LastModified>2000-01-01T00:00:00.000Z</LastModified><ETag>"a"</ETag><Size>1</Size><StorageClass>STANDARD</StorageClass></Contents>
<Contents><Key>content/fresh.pdf</Key><LastModified>2099-01-01T00:00:00.000Z</LastModified><ETag>"b"</ETag><Size>1</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`
		case http.MethodDelete:
			deleted = append(deleted, request.URL.Path)
			status = http.StatusNoContent
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	store, err := New(context.Background(), Config{
		Endpoint:        "https://s3.internal",
		PublicEndpoint:  "https://s3.example",
		Region:          "us-east-1",
		Bucket:          "latexgrambot",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Prefix:          "content",
		PresignTTL:      time.Hour,
		HTTPClient:      client,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	removed, err := store.DeleteExpired(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(deleted) != 1 || !strings.HasSuffix(deleted[0], "/content/old.jpg") {
		t.Fatalf("deleted = %v, want the expired object only", deleted)
	}
}

func TestS3Integration(t *testing.T) {
	endpoint := os.Getenv("LATEXGRAMBOT_S3_TEST_ENDPOINT")
	accessKey := os.Getenv("LATEXGRAMBOT_S3_TEST_ACCESS_KEY")
	secretKey := os.Getenv("LATEXGRAMBOT_S3_TEST_SECRET_KEY")
	if endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("S3 integration environment is not configured")
	}
	store, err := New(context.Background(), Config{
		Endpoint:        endpoint,
		PublicEndpoint:  endpoint,
		Region:          "us-east-1",
		Bucket:          "latexgrambot",
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		Prefix:          "integration",
		PresignTTL:      5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const payload = "latexgrambot S3 integration probe\n"
	signed, err := store.Put(context.Background(), "txt", "text/plain", []byte(payload))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	response, err := http.Get(signed)
	if err != nil {
		t.Fatalf("GET presigned URL: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read presigned response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != payload {
		t.Fatalf("presigned GET = %s, body %q", response.Status, body)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
