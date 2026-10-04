// Package files stores rendered outputs in a private S3-compatible bucket
// and issues short-lived presigned URLs for Telegram inline-query results.
package files

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

// Config points the store at the internal S3-compatible API and the public
// API endpoint used only when signing download URLs.
type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	Prefix          string
	PublicEndpoint  string
	PresignTTL      time.Duration
	HTTPClient      aws.HTTPClient
}

// Store keeps content-addressed rendered assets in S3.
type Store struct {
	client     *s3.Client
	presigner  *s3.PresignClient
	bucket     string
	prefix     string
	presignTTL time.Duration
}

// New builds an S3-compatible object store.
func New(ctx context.Context, cfg Config) (*Store, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if cfg.HTTPClient != nil {
		awsCfg.HTTPClient = cfg.HTTPClient
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		options.UsePathStyle = true
	})
	publicClient := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.PublicEndpoint)
		options.UsePathStyle = true
	})
	return &Store{
		client:     client,
		presigner:  s3.NewPresignClient(publicClient),
		bucket:     cfg.Bucket,
		prefix:     strings.Trim(cfg.Prefix, "/"),
		presignTTL: cfg.PresignTTL,
	}, nil
}

// Put uploads the data under a fresh UUIDv4 object name and returns a
// short-lived presigned GET URL.
func (s *Store) Put(ctx context.Context, ext, contentType string, data []byte) (string, error) {
	key := s.key(uuid.NewString(), ext)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("private, max-age=86400"),
	})
	if err != nil {
		return "", fmt.Errorf("put %s object: %w", ext, err)
	}

	request, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, func(options *s3.PresignOptions) {
		options.Expires = s.presignTTL
	})
	if err != nil {
		return "", fmt.Errorf("presign %s object: %w", ext, err)
	}
	return request.URL, nil
}

// DeleteExpired removes objects older than age and returns how many were
// deleted. Telegram downloads the file when an inline result is picked and
// the presigned link expires after the presign TTL; objects past their
// lifetime can no longer be fetched, so they are garbage collected.
func (s *Store) DeleteExpired(ctx context.Context, age time.Duration) (int, error) {
	cutoff := time.Now().Add(-age)
	prefix := ""
	if s.prefix != "" {
		prefix = s.prefix + "/"
	}
	deleted := 0
	var token *string
	for {
		list, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return deleted, fmt.Errorf("list objects: %w", err)
		}
		for _, object := range list.Contents {
			if object.LastModified == nil || !object.LastModified.Before(cutoff) {
				continue
			}
			if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    object.Key,
			}); err != nil {
				return deleted, fmt.Errorf("delete %s: %w", aws.ToString(object.Key), err)
			}
			deleted++
		}
		if list.IsTruncated == nil || !*list.IsTruncated {
			return deleted, nil
		}
		token = list.NextContinuationToken
	}
}

func (s *Store) key(name, ext string) string {
	if s.prefix == "" {
		return name + "." + ext
	}
	return s.prefix + "/" + name + "." + ext
}
