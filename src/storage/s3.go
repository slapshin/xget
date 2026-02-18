package storage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"xget/src/config"
)

// S3Source implements Source for S3/MinIO storage.
type S3Source struct {
	client *s3.Client
	bucket string
	key    string
}

func newS3Source(url string, aliases map[string]config.Alias) (*S3Source, error) {
	// Parse s3://alias/path format.
	aliasName, key, err := parseS3URL(url)
	if err != nil {
		return nil, err
	}

	alias, exists := aliases[aliasName]
	if !exists {
		return nil, fmt.Errorf("alias %q not found", aliasName)
	}

	client, err := createS3Client(context.Background(), alias)
	if err != nil {
		return nil, fmt.Errorf("creating S3 client: %w", err)
	}

	// Prepend prefix to key if configured.
	fullKey := key
	if alias.Prefix != "" {
		fullKey = alias.Prefix + key
	}

	return &S3Source{
		client: client,
		bucket: alias.Bucket,
		key:    fullKey,
	}, nil
}

// NewS3SourceFromAlias creates an S3Source directly from an alias and key.
func NewS3SourceFromAlias(ctx context.Context, alias config.Alias, key string) (*S3Source, error) {
	client, err := createS3Client(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("creating S3 client: %w", err)
	}

	fullKey := key
	if alias.Prefix != "" {
		fullKey = alias.Prefix + key
	}

	return &S3Source{
		client: client,
		bucket: alias.Bucket,
		key:    fullKey,
	}, nil
}

// parseS3URL parses s3://alias/path into alias name and path.
func parseS3URL(url string) (string, string, error) {
	// Remove s3:// prefix.
	withoutScheme := strings.TrimPrefix(url, "s3://")

	parts := strings.SplitN(withoutScheme, "/", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid s3 URL format: %s (expected s3://alias/path)", url)
	}

	return parts[0], parts[1], nil
}

func createS3Client(ctx context.Context, alias config.Alias) (*s3.Client, error) {
	var opts []func(*awsconfig.LoadOptions) error

	// Set region if provided.
	if alias.Region != "" {
		opts = append(opts, awsconfig.WithRegion(alias.Region))
	}

	if alias.NoSignRequest {
		opts = append(opts, awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}))
	} else if alias.AccessKey != "" && alias.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(alias.AccessKey, alias.SecretKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	// Create S3 client with custom endpoint for MinIO support.
	clientOpts := []func(*s3.Options){}
	if alias.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(alias.Endpoint)
			o.UsePathStyle = true // Required for MinIO.
		})
	}

	return s3.NewFromConfig(cfg, clientOpts...), nil
}

// Download retrieves the file content starting from the given offset.
func (s3Source *S3Source) Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s3Source.bucket),
		Key:    aws.String(s3Source.key),
	}

	// Set Range header for resume support.
	if offset > 0 {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-", offset))
	}

	result, err := s3Source.client.GetObject(ctx, input)
	if err != nil {
		return nil, 0, fmt.Errorf("getting object: %w", err)
	}

	// Calculate total size.
	var totalSize int64

	if result.ContentRange != nil {
		// Parse Content-Range for total size.
		var start, end int64

		_, parseErr := fmt.Sscanf(*result.ContentRange, "bytes %d-%d/%d", &start, &end, &totalSize)
		if parseErr != nil {
			totalSize = offset + *result.ContentLength
		}
	} else if result.ContentLength != nil {
		totalSize = *result.ContentLength
	}

	return result.Body, totalSize, nil
}

// GetSize returns the total size of the file.
func (s3Source *S3Source) GetSize(ctx context.Context) (int64, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(s3Source.bucket),
		Key:    aws.String(s3Source.key),
	}

	result, err := s3Source.client.HeadObject(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("head object: %w", err)
	}

	if result.ContentLength == nil {
		return 0, fmt.Errorf("content length not available")
	}

	return *result.ContentLength, nil
}

// Upload uploads content to S3.
func (s3Source *S3Source) Upload(ctx context.Context, reader io.Reader) error {
	_, err := s3Source.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s3Source.bucket),
		Key:    aws.String(s3Source.key),
		Body:   reader,
	})
	if err != nil {
		return fmt.Errorf("putting object: %w", err)
	}

	return nil
}

// Exists checks if the object exists in S3.
func (s3Source *S3Source) Exists(ctx context.Context) (bool, error) {
	_, err := s3Source.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s3Source.bucket),
		Key:    aws.String(s3Source.key),
	})
	if err != nil {
		// Check if it's a "not found" error.
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
			return false, nil
		}

		return false, fmt.Errorf("head object: %w", err)
	}

	return true, nil
}
