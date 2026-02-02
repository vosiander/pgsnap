package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/vosiander/pgsnap/pkg/common"
)

// S3Uploader handles uploading backups to S3
type S3Uploader struct {
	client *s3.Client
	config *common.S3Config
}

// NewS3Uploader creates a new S3 uploader
func NewS3Uploader(s3Config *common.S3Config) (*S3Uploader, error) {
	// Create AWS config
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(s3Config.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			s3Config.AccessKey,
			s3Config.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with custom endpoint
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Config.Endpoint)
		o.UsePathStyle = true
	})

	return &S3Uploader{
		client: client,
		config: s3Config,
	}, nil
}

// Upload uploads a file to S3
func (u *S3Uploader) Upload(ctx context.Context, filePath string) (string, error) {
	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Build S3 key
	filename := filepath.Base(filePath)
	key := filename
	if u.config.Prefix != "" {
		key = filepath.Join(u.config.Prefix, filename)
	}

	// Upload file
	_, err = u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(u.config.Bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to S3: %w", err)
	}

	// Build S3 URL
	s3URL := fmt.Sprintf("s3://%s/%s", u.config.Bucket, key)
	return s3URL, nil
}
