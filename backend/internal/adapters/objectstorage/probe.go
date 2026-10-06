// Package objectstorage adapts private S3-compatible artifact storage.
package objectstorage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
	appconfig "github.com/SinDenis/drip-real-estate-demo/backend/internal/platform/config"
)

// Store persists raw source artifacts and checks access to its configured bucket.
type Store struct {
	client *s3.Client
	bucket string
}

// New configures an S3-compatible readiness probe without contacting storage.
func New(ctx context.Context, settings appconfig.S3Settings) (*Store, error) {
	provider := credentials.NewStaticCredentialsProvider(
		settings.AccessKeyID, settings.SecretAccessKey, "",
	)
	awsSettings, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(settings.Region), awsconfig.WithCredentialsProvider(provider))
	if err != nil {
		return nil, fmt.Errorf("configure S3 client: %w", err)
	}
	client := s3.NewFromConfig(awsSettings, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(settings.Endpoint)
		options.UsePathStyle = settings.PathStyle
	})
	return &Store{client: client, bucket: settings.Bucket}, nil
}

// Check verifies that the configured bucket is accessible.
func (probe *Store) Check(ctx context.Context) error {
	_, err := probe.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &probe.bucket})
	if err != nil {
		return fmt.Errorf("check S3 bucket: %w", err)
	}
	return nil
}

// PutRaw writes one exact private source response with immutable provenance metadata.
func (probe *Store) PutRaw(
	ctx context.Context,
	artifact application.RawArtifact,
	body []byte,
) error {
	if int64(len(body)) != artifact.Size {
		return fmt.Errorf("store raw artifact %q: byte size does not match metadata", artifact.Key)
	}
	_, err := probe.client.PutObject(ctx, rawObjectInput(probe.bucket, artifact, body))
	if err == nil {
		return nil
	}
	var responseError *smithyhttp.ResponseError
	if !errors.As(err, &responseError) || responseError.HTTPStatusCode() != 412 {
		return fmt.Errorf("store raw artifact %q: %w", artifact.Key, err)
	}
	return probe.verifyRawObject(ctx, artifact)
}

func rawObjectInput(
	bucket string,
	artifact application.RawArtifact,
	body []byte,
) *s3.PutObjectInput {
	return &s3.PutObjectInput{
		Bucket:      &bucket,
		Key:         &artifact.Key,
		Body:        bytes.NewReader(body),
		ContentType: &artifact.ContentType,
		IfNoneMatch: aws.String("*"),
		Metadata: map[string]string{
			"sha256":      artifact.SHA256,
			"observed-at": artifact.ObservedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
			"byte-size":   strconv.FormatInt(artifact.Size, 10),
		},
	}
}

func (probe *Store) verifyRawObject(ctx context.Context, artifact application.RawArtifact) error {
	existing, headErr := probe.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &probe.bucket, Key: &artifact.Key,
	})
	if headErr != nil {
		return fmt.Errorf("verify existing raw artifact %q: %w", artifact.Key, headErr)
	}
	if existing.Metadata["sha256"] != artifact.SHA256 || existing.ContentLength == nil ||
		*existing.ContentLength != artifact.Size {
		return fmt.Errorf("raw artifact %q already exists with different content", artifact.Key)
	}
	return nil
}
