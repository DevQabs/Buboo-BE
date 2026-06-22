// Package storage provides S3-compatible storage operations for file uploads.
package storage

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// SupabaseStorage uploads/deletes files via S3-compatible API.
type SupabaseStorage struct {
	client     *s3.Client
	bucket     string
	publicBase string // public URL base for constructing file URLs
}

func New(endpoint, accessKeyID, secretAccessKey, bucket, publicBase string) *SupabaseStorage {
	cfg := aws.Config{
		Region: "ap-southeast-2",
		Credentials: credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		EndpointResolverWithOptions: aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: endpoint, HostnameImmutable: true}, nil
			},
		),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})
	return &SupabaseStorage{client: client, bucket: bucket, publicBase: publicBase}
}

// Upload sends file bytes to S3-compatible storage and returns the public URL.
func (s *SupabaseStorage) Upload(filename string, data []byte, contentType string) (string, error) {
	_, err := s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(filename),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("s3 upload failed: %w", err)
	}
	return s.PublicURL(filename), nil
}

// Delete removes a file from S3-compatible storage.
func (s *SupabaseStorage) Delete(filename string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(filename),
	})
	if err != nil {
		return fmt.Errorf("s3 delete failed: %w", err)
	}
	return nil
}

// PublicURL constructs the public URL for a given filename.
func (s *SupabaseStorage) PublicURL(filename string) string {
	return fmt.Sprintf("%s/%s/%s", s.publicBase, s.bucket, filename)
}
