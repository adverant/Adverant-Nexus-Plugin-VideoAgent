package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// HTTPDownloader handles robust HTTP file downloads with retry logic
type HTTPDownloader struct {
	client         *http.Client
	maxRetries     int
	retryDelay     time.Duration
	timeout        time.Duration
	maxFileSize    int64 // Maximum file size in bytes (0 = unlimited)
	allowedTypes   []string
	tempDir        string
}

// HTTPDownloaderConfig holds configuration for HTTP downloader
type HTTPDownloaderConfig struct {
	MaxRetries   int           // Default: 3
	RetryDelay   time.Duration // Default: 2s
	Timeout      time.Duration // Default: 5min
	MaxFileSize  int64         // Default: 5GB
	AllowedTypes []string      // Default: ["video/"]
	TempDir      string        // Default: /tmp
}

// NewHTTPDownloader creates a new HTTP downloader with default configuration
func NewHTTPDownloader(config *HTTPDownloaderConfig) *HTTPDownloader {
	if config == nil {
		config = &HTTPDownloaderConfig{}
	}

	// Set defaults
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 2 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 5 * 1024 * 1024 * 1024 // 5GB default
	}
	if len(config.AllowedTypes) == 0 {
		config.AllowedTypes = []string{"video/"} // Accept any video/* MIME type
	}
	if config.TempDir == "" {
		config.TempDir = "/tmp"
	}

	return &HTTPDownloader{
		client: &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 10 redirects
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		maxRetries:   config.MaxRetries,
		retryDelay:   config.RetryDelay,
		timeout:      config.Timeout,
		maxFileSize:  config.MaxFileSize,
		allowedTypes: config.AllowedTypes,
		tempDir:      config.TempDir,
	}
}

// DownloadFile downloads a file from URL with retry logic
func (d *HTTPDownloader) DownloadFile(ctx context.Context, url, jobID string) (string, error) {
	var lastErr error

	for attempt := 1; attempt <= d.maxRetries; attempt++ {
		filePath, err := d.downloadAttempt(ctx, url, jobID)
		if err == nil {
			return filePath, nil
		}

		lastErr = err

		// Don't retry on validation errors (wrong content type, file too large)
		if !d.isRetryableError(err) {
			return "", fmt.Errorf("download failed (non-retryable): %w", err)
		}

		// Don't sleep after last attempt
		if attempt < d.maxRetries {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(d.retryDelay * time.Duration(attempt)): // Exponential backoff
			}
		}
	}

	return "", fmt.Errorf("download failed after %d attempts: %w", d.maxRetries, lastErr)
}

// downloadAttempt performs a single download attempt
func (d *HTTPDownloader) downloadAttempt(ctx context.Context, url, jobID string) (string, error) {
	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent
	req.Header.Set("User-Agent", "VideoAgent-Worker/1.0")

	// Execute request
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", &HTTPError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status),
		}
	}

	// Validate Content-Type
	contentType := resp.Header.Get("Content-Type")
	if !d.isAllowedContentType(contentType) {
		return "", &ValidationError{
			Field:   "Content-Type",
			Value:   contentType,
			Message: fmt.Sprintf("unsupported content type: %s (expected video/*)", contentType),
		}
	}

	// Check Content-Length if available
	if resp.ContentLength > 0 && resp.ContentLength > d.maxFileSize {
		return "", &ValidationError{
			Field:   "Content-Length",
			Value:   fmt.Sprintf("%d bytes", resp.ContentLength),
			Message: fmt.Sprintf("file too large: %d bytes (max: %d bytes)", resp.ContentLength, d.maxFileSize),
		}
	}

	// Create temporary file
	tempFile, err := d.createTempFile(jobID)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	// Download with size limit check
	_, err = d.copyWithLimit(tempFile, resp.Body, d.maxFileSize)
	if err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Close file
	if err := tempFile.Close(); err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to close file: %w", err)
	}

	return tempFile.Name(), nil
}

// createTempFile creates a temporary file for download
func (d *HTTPDownloader) createTempFile(jobID string) (*os.File, error) {
	// Ensure temp directory exists
	if err := os.MkdirAll(d.tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create temp file with job ID prefix
	pattern := fmt.Sprintf("videoagent-%s-*.tmp", jobID)
	tempFile, err := os.CreateTemp(d.tempDir, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	return tempFile, nil
}

// copyWithLimit copies data with size limit
func (d *HTTPDownloader) copyWithLimit(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	limitedReader := io.LimitReader(src, limit+1) // +1 to detect overflow
	written, err := io.Copy(dst, limitedReader)
	if err != nil {
		return written, err
	}

	if written > limit {
		return written, &ValidationError{
			Field:   "file_size",
			Value:   fmt.Sprintf("%d bytes", written),
			Message: fmt.Sprintf("file exceeded size limit: %d bytes (max: %d bytes)", written, limit),
		}
	}

	return written, nil
}

// isAllowedContentType checks if content type is allowed
func (d *HTTPDownloader) isAllowedContentType(contentType string) bool {
	if contentType == "" {
		// Allow empty content type (some servers don't set it)
		return true
	}

	for _, allowed := range d.allowedTypes {
		if len(contentType) >= len(allowed) && contentType[:len(allowed)] == allowed {
			return true
		}
	}

	return false
}

// isRetryableError checks if error is retryable
func (d *HTTPDownloader) isRetryableError(err error) bool {
	// Don't retry validation errors
	if _, ok := err.(*ValidationError); ok {
		return false
	}

	// Don't retry 4xx client errors
	if httpErr, ok := err.(*HTTPError); ok {
		return httpErr.StatusCode >= 500 // Only retry 5xx server errors
	}

	// Retry network errors
	return true
}

// CleanupFile removes downloaded file
func (d *HTTPDownloader) CleanupFile(filePath string) error {
	if filePath == "" {
		return nil
	}

	// Only delete files in temp directory
	if !filepath.HasPrefix(filePath, d.tempDir) {
		return fmt.Errorf("refusing to delete file outside temp directory: %s", filePath)
	}

	return os.Remove(filePath)
}

// HTTPError represents an HTTP error
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return e.Message
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Value   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s (value: %s)", e.Field, e.Message, e.Value)
}
