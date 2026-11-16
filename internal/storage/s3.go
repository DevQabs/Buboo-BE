package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Config points at one bucket of any S3-compatible store (Cloudflare R2 now).
// Region "" means "auto" (R2). Nothing is hardcoded so the bucket can move to
// another S3-compatible store by changing env vars only (설계서 5절).
type Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Region          string
}

// ErrNotFound is returned by Head when the object does not exist.
var ErrNotFound = errors.New("object not found")

// Client uses only the standard S3 API.
type Client struct {
	s3      *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func NewClient(c Config) *Client {
	region := c.Region
	if region == "" {
		region = "auto"
	}
	cl := s3.New(s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""),
		BaseEndpoint: aws.String(c.Endpoint),
		UsePathStyle: true,
		// 체크섬은 필요할 때만. presigned PUT 주소에 체크섬 파라미터가 붙지 않게 한다
		// (브라우저 PUT 은 그 값을 맞춰 보낼 수 없다). s3_test.go 가 확인한다.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &Client{s3: cl, presign: s3.NewPresignClient(cl), bucket: c.Bucket}
}

// PresignPut signs a PUT for exactly this content type and size. The browser
// must send the same Content-Type header.
func (c *Client) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// PresignGet signs a GET locally (no network call).
func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// Head returns the object's size, or ErrNotFound.
func (c *Client) Head(ctx context.Context, key string) (int64, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		var nf *types.NotFound
		if errors.As(err, &nf) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return aws.ToInt64(out.ContentLength), nil
}

// Get reads a whole object (used by cmd/migrate-storage).
func (c *Client) Get(ctx context.Context, key string) ([]byte, string, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, "", err
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	return data, aws.ToString(out.ContentType), err
}

// Put writes a whole object (used by cmd/migrate-storage).
func (c *Client) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(data))),
	})
	return err
}

// Delete removes an object. Deleting a missing key is not an error in S3.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	return err
}
