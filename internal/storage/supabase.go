// Package storage provides Supabase Storage operations for file uploads.
package storage

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const bucket = "diary-photos"

// SupabaseStorage uploads/deletes files via Supabase Storage REST API.
type SupabaseStorage struct {
	projectURL string // e.g. https://xxx.supabase.co
	serviceKey string // service_role key
	client     *http.Client
}

func New(projectURL, serviceKey string) *SupabaseStorage {
	return &SupabaseStorage{
		projectURL: strings.TrimRight(projectURL, "/"),
		serviceKey: serviceKey,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Upload sends file bytes to Supabase Storage and returns the public URL.
func (s *SupabaseStorage) Upload(filename string, data []byte, contentType string) (string, error) {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.projectURL, bucket, filename)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "true")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("storage upload failed %d: %s", resp.StatusCode, body)
	}

	publicURL := fmt.Sprintf("%s/storage/v1/object/public/%s/%s", s.projectURL, bucket, filename)
	return publicURL, nil
}

// Delete removes a file from Supabase Storage by its filename (path within bucket).
func (s *SupabaseStorage) Delete(filename string) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.projectURL, bucket, filename)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("storage delete failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

// PublicURL constructs the public URL for a given filename.
func (s *SupabaseStorage) PublicURL(filename string) string {
	return fmt.Sprintf("%s/storage/v1/object/public/%s/%s", s.projectURL, bucket, filename)
}
