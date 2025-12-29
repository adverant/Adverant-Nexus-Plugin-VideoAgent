package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// YouTubeAuthClient is an interface for fetching YouTube OAuth tokens
// Implemented by clients.NexusAuthClient in production
type YouTubeAuthClient interface {
	GetYouTubeToken(ctx context.Context, userID string) (*YouTubeToken, error)
}

// YouTubeToken represents a YouTube OAuth token
type YouTubeToken struct {
	AccessToken string
	ExpiresAt   time.Time
	Scopes      []string
	ChannelID   string
	IsValid     bool
}

// YouTubeDownloader handles downloading videos from YouTube using yt-dlp
type YouTubeDownloader struct {
	ytdlpPath   string
	outputDir   string
	cookiesPath string // Optional path to cookies file for authenticated downloads
	proxyURL    string // Optional proxy URL (residential/ISP proxies work best)
	authClient  YouTubeAuthClient // Optional client for fetching OAuth tokens
}

// NewYouTubeDownloader creates a new YouTube downloader
// Environment variables:
// - YOUTUBE_COOKIES_PATH: Path to Netscape-format cookies file
// - YOUTUBE_PROXY_URL: Proxy URL (http://user:pass@host:port or socks5://...)
func NewYouTubeDownloader(outputDir string) (*YouTubeDownloader, error) {
	// Verify yt-dlp installation
	ytdlpPath, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil, fmt.Errorf("yt-dlp not found in PATH (required for YouTube support): %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get optional configuration from environment
	cookiesPath := os.Getenv("YOUTUBE_COOKIES_PATH")
	proxyURL := os.Getenv("YOUTUBE_PROXY_URL")

	// Log configuration for debugging
	if cookiesPath != "" {
		log.Printf("[YouTubeDownloader] Using cookies file: %s", cookiesPath)
	}
	if proxyURL != "" {
		// Mask password in log
		maskedProxy := maskProxyCredentials(proxyURL)
		log.Printf("[YouTubeDownloader] Using proxy: %s", maskedProxy)
	}

	return &YouTubeDownloader{
		ytdlpPath:   ytdlpPath,
		outputDir:   outputDir,
		cookiesPath: cookiesPath,
		proxyURL:    proxyURL,
		authClient:  nil, // Set via SetAuthClient() if needed
	}, nil
}

// SetAuthClient sets the YouTube OAuth client for authenticated downloads
func (yd *YouTubeDownloader) SetAuthClient(client YouTubeAuthClient) {
	yd.authClient = client
}

// maskProxyCredentials hides password in proxy URL for logging
func maskProxyCredentials(proxyURL string) string {
	// Simple masking: replace password portion
	if strings.Contains(proxyURL, "@") {
		parts := strings.SplitN(proxyURL, "@", 2)
		if len(parts) == 2 {
			protoUser := parts[0]
			hostPort := parts[1]
			if idx := strings.LastIndex(protoUser, ":"); idx > 0 {
				// Find the protocol part
				if protoIdx := strings.Index(protoUser, "://"); protoIdx > 0 {
					proto := protoUser[:protoIdx+3]
					userPass := protoUser[protoIdx+3:]
					if colonIdx := strings.Index(userPass, ":"); colonIdx > 0 {
						user := userPass[:colonIdx]
						return proto + user + ":****@" + hostPort
					}
				}
			}
		}
	}
	return proxyURL
}

// IsYouTubeURL checks if the given URL is a YouTube URL
func IsYouTubeURL(url string) bool {
	url = strings.ToLower(url)
	return strings.Contains(url, "youtube.com") ||
		strings.Contains(url, "youtu.be") ||
		strings.Contains(url, "youtube-nocookie.com")
}

// Download downloads a YouTube video and returns the local file path
func (yd *YouTubeDownloader) Download(ctx context.Context, url, jobID string) (string, error) {
	if !IsYouTubeURL(url) {
		return "", fmt.Errorf("not a valid YouTube URL: %s", url)
	}

	// Output filename with job ID for tracking
	outputTemplate := filepath.Join(yd.outputDir, fmt.Sprintf("%s_youtube.%%(ext)s", jobID))

	// yt-dlp arguments for optimal video download
	// Note: We use multiple anti-bot workarounds to avoid YouTube's bot detection
	// YouTube aggressively blocks data center IPs - residential/ISP proxies required
	args := []string{
		url,
		"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best", // Prefer mp4
		"-o", outputTemplate,
		"--no-playlist",                // Only download single video, not playlist
		"--no-warnings",                // Suppress warnings
		"--no-call-home",               // Don't contact yt-dlp servers
		"--no-check-certificate",       // Skip certificate validation (for some proxies)
		"--prefer-ffmpeg",              // Use ffmpeg for merging
		"--merge-output-format", "mp4", // Always output mp4
		"--restrict-filenames",         // Avoid special characters in filenames
	}

	// CRITICAL: Add proxy if configured (residential/ISP proxies work, datacenter proxies do NOT)
	// YouTube actively blocks datacenter IPs - this is the primary solution
	if yd.proxyURL != "" {
		args = append(args, "--proxy", yd.proxyURL)
		log.Printf("[YouTubeDownloader] Using proxy for download: %s", maskProxyCredentials(yd.proxyURL))
	}

	// CRITICAL: Add cookies if configured (for authenticated downloads)
	// This bypasses bot detection by using a real user's session
	if yd.cookiesPath != "" {
		if _, err := os.Stat(yd.cookiesPath); err == nil {
			args = append(args, "--cookies", yd.cookiesPath)
			log.Printf("[YouTubeDownloader] Using cookies file: %s", yd.cookiesPath)
		} else {
			log.Printf("[YouTubeDownloader] WARNING: Cookies file not found: %s", yd.cookiesPath)
		}
	}

	// Anti-bot workarounds (fallback strategies when no proxy/cookies)
	args = append(args,
		// Strategy 1: Use mweb (mobile web) client which has fewer restrictions
		"--extractor-args", "youtube:player_client=mweb",

		// Strategy 2: Use iOS Safari user agent (often less blocked than Android)
		"--user-agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",

		// Strategy 3: Standard browser headers
		"--add-header", "Accept-Language:en-US,en;q=0.9",
		"--add-header", "Accept:text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",

		// Strategy 4: Rate limiting to appear more human
		"--sleep-requests", "2",        // Slower requests
		"--sleep-interval", "3",        // Sleep between downloads
		"--max-sleep-interval", "6",    // Random sleep up to 6s

		// Strategy 5: Retry with different extractors
		"--extractor-retries", "5",     // Retry extractor 5 times
		"--retries", "5",               // Retry download 5 times
		"--fragment-retries", "5",      // Retry fragments

		// Strategy 6: Age gate bypass attempt
		"--age-limit", "21",            // Set age limit high

		// Verbose output for debugging
		"-v",
	)

	// Log the download attempt
	log.Printf("[YouTubeDownloader] Starting download for job %s: %s", jobID, url)

	// Create command with context for cancellation support
	cmd := exec.CommandContext(ctx, yd.ytdlpPath, args...)

	// Capture output for error reporting
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"yt-dlp failed to download video: %w\nOutput: %s\nURL: %s",
			err, string(output), url,
		)
	}

	// Find the downloaded file (yt-dlp replaces %(ext)s with actual extension)
	// Most common extensions: mp4, webm, mkv
	possibleExtensions := []string{"mp4", "webm", "mkv", "avi", "mov"}
	var downloadedFile string

	for _, ext := range possibleExtensions {
		candidate := filepath.Join(yd.outputDir, fmt.Sprintf("%s_youtube.%s", jobID, ext))
		if _, err := os.Stat(candidate); err == nil {
			downloadedFile = candidate
			break
		}
	}

	if downloadedFile == "" {
		return "", fmt.Errorf(
			"yt-dlp completed but downloaded file not found. "+
				"Expected pattern: %s_youtube.[mp4|webm|mkv]. "+
				"Directory: %s",
			jobID, yd.outputDir,
		)
	}

	return downloadedFile, nil
}

// DownloadWithUserAuth downloads a YouTube video using user's OAuth token for authenticated access
// This method:
// 1. Fetches the user's YouTube OAuth token from Nexus-Auth
// 2. Uses the access token with yt-dlp for authenticated download
// 3. Falls back to unauthenticated download if token unavailable
//
// Authenticated downloads provide:
// - Access to private/unlisted videos the user owns
// - Higher quota limits
// - Better rate limiting treatment
// - Access to age-restricted content
//
// Parameters:
//   - ctx: Context for cancellation
//   - url: YouTube video URL
//   - jobID: Job ID for file naming
//   - userID: User ID to fetch OAuth token for
//
// Returns:
//   - string: Path to downloaded video file
//   - error: Error if download fails
func (yd *YouTubeDownloader) DownloadWithUserAuth(ctx context.Context, url, jobID, userID string) (string, error) {
	if !IsYouTubeURL(url) {
		return "", fmt.Errorf("not a valid YouTube URL: %s", url)
	}

	// Strategy 1: Try authenticated download if auth client available
	if yd.authClient != nil && userID != "" {
		log.Printf("[YouTubeDownloader] Attempting authenticated download for user %s", userID)

		// Fetch YouTube OAuth token from Nexus-Auth
		token, err := yd.authClient.GetYouTubeToken(ctx, userID)
		if err != nil {
			// Log the error but don't fail - fall back to unauthenticated
			log.Printf("[YouTubeDownloader] Failed to get YouTube token for user %s: %v", userID, err)
			log.Printf("[YouTubeDownloader] Falling back to unauthenticated download")
		} else if token != nil && token.IsValid {
			// Attempt authenticated download with OAuth token
			filePath, authErr := yd.downloadWithToken(ctx, url, jobID, token.AccessToken)
			if authErr == nil {
				log.Printf("[YouTubeDownloader] ✓ Authenticated download successful for user %s", userID)
				return filePath, nil
			}

			// Authenticated download failed - log and fall back
			log.Printf("[YouTubeDownloader] Authenticated download failed: %v", authErr)
			log.Printf("[YouTubeDownloader] Falling back to unauthenticated download")
		}
	}

	// Strategy 2: Fall back to unauthenticated download
	// This uses existing cookies/proxy method
	log.Printf("[YouTubeDownloader] Using unauthenticated download for job %s", jobID)
	return yd.Download(ctx, url, jobID)
}

// downloadWithToken performs authenticated YouTube download using OAuth access token
func (yd *YouTubeDownloader) downloadWithToken(ctx context.Context, url, jobID, accessToken string) (string, error) {
	// Output filename with job ID for tracking
	outputTemplate := filepath.Join(yd.outputDir, fmt.Sprintf("%s_youtube.%%(ext)s", jobID))

	// yt-dlp arguments for authenticated download
	args := []string{
		url,
		"-f", "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best", // Prefer mp4
		"-o", outputTemplate,
		"--no-playlist",                // Only download single video
		"--no-warnings",
		"--no-call-home",
		"--no-check-certificate",
		"--prefer-ffmpeg",
		"--merge-output-format", "mp4",
		"--restrict-filenames",
	}

	// CRITICAL: Add OAuth token as Authorization header
	// This provides authenticated access to YouTube API
	args = append(args,
		"--add-header", fmt.Sprintf("Authorization: Bearer %s", accessToken),
	)

	// Add proxy if configured (works alongside OAuth)
	if yd.proxyURL != "" {
		args = append(args, "--proxy", yd.proxyURL)
		log.Printf("[YouTubeDownloader] Using proxy with OAuth: %s", maskProxyCredentials(yd.proxyURL))
	}

	// Use standard YouTube client (OAuth handles authentication)
	args = append(args,
		"--extractor-args", "youtube:player_client=web",

		// Standard headers
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"--add-header", "Accept-Language:en-US,en;q=0.9",
		"--add-header", "Accept:text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",

		// Retry configuration
		"--extractor-retries", "3",
		"--retries", "3",
		"--fragment-retries", "3",

		// Verbose output for debugging
		"-v",
	)

	// Log the download attempt (don't log token)
	log.Printf("[YouTubeDownloader] Starting authenticated download for job %s: %s", jobID, url)

	// Create command with context for cancellation support
	cmd := exec.CommandContext(ctx, yd.ytdlpPath, args...)

	// Capture output for error reporting
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"yt-dlp authenticated download failed: %w\nOutput: %s\nURL: %s",
			err, string(output), url,
		)
	}

	// Find the downloaded file
	possibleExtensions := []string{"mp4", "webm", "mkv", "avi", "mov"}
	var downloadedFile string

	for _, ext := range possibleExtensions {
		candidate := filepath.Join(yd.outputDir, fmt.Sprintf("%s_youtube.%s", jobID, ext))
		if _, err := os.Stat(candidate); err == nil {
			downloadedFile = candidate
			break
		}
	}

	if downloadedFile == "" {
		return "", fmt.Errorf(
			"yt-dlp completed but downloaded file not found. "+
				"Expected pattern: %s_youtube.[mp4|webm|mkv]. "+
				"Directory: %s",
			jobID, yd.outputDir,
		)
	}

	return downloadedFile, nil
}

// DownloadWithMetadata downloads a YouTube video and returns path with metadata
// Uses a hybrid approach:
// 1. YouTube Data API v3 for metadata (official API, no proxy needed)
// 2. yt-dlp with proxy for actual video download
func (yd *YouTubeDownloader) DownloadWithMetadata(ctx context.Context, url, jobID string) (string, map[string]interface{}, error) {
	var metadata map[string]interface{}

	// Strategy 1: Try YouTube Data API v3 first (preferred - official API)
	apiClient, apiErr := NewYouTubeAPIClient()
	if apiErr == nil {
		log.Printf("[YouTubeDownloader] Using YouTube Data API for metadata extraction")
		apiMetadata, err := apiClient.GetVideoMetadata(ctx, url)
		if err == nil {
			metadata = apiMetadata.ToMap()
			log.Printf("[YouTubeDownloader] Successfully fetched metadata via API: %s", apiMetadata.Title)
		} else {
			log.Printf("[YouTubeDownloader] API metadata extraction failed: %v (falling back to yt-dlp)", err)
		}
	} else {
		log.Printf("[YouTubeDownloader] YouTube API not configured: %v (using yt-dlp for metadata)", apiErr)
	}

	// Strategy 2: Fallback to yt-dlp for metadata if API didn't work
	if metadata == nil {
		var err error
		metadata, err = yd.ExtractMetadata(ctx, url)
		if err != nil {
			// Non-fatal: proceed with download even if metadata extraction fails
			log.Printf("[YouTubeDownloader] yt-dlp metadata extraction also failed: %v", err)
			metadata = make(map[string]interface{})
		}
	}

	// Download the video using yt-dlp with proxy
	filePath, err := yd.Download(ctx, url, jobID)
	if err != nil {
		return "", nil, err
	}

	return filePath, metadata, nil
}

// ExtractMetadata extracts video metadata without downloading
func (yd *YouTubeDownloader) ExtractMetadata(ctx context.Context, url string) (map[string]interface{}, error) {
	args := []string{
		url,
		"--dump-json",     // Output metadata as JSON
		"--no-playlist",   // Single video only
		"--no-warnings",
		"--no-call-home",
	}

	// Add proxy if configured
	if yd.proxyURL != "" {
		args = append(args, "--proxy", yd.proxyURL)
	}

	// Add cookies if configured
	if yd.cookiesPath != "" {
		if _, err := os.Stat(yd.cookiesPath); err == nil {
			args = append(args, "--cookies", yd.cookiesPath)
		}
	}

	cmd := exec.CommandContext(ctx, yd.ytdlpPath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to extract YouTube metadata: %w", err)
	}

	// Parse JSON metadata
	var metadata map[string]interface{}
	if err := parseJSON(output, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse YouTube metadata JSON: %w", err)
	}

	// Extract commonly used fields
	result := make(map[string]interface{})

	if title, ok := metadata["title"].(string); ok {
		result["youtube_title"] = title
	}
	if duration, ok := metadata["duration"].(float64); ok {
		result["youtube_duration"] = duration
	}
	if width, ok := metadata["width"].(float64); ok {
		result["youtube_width"] = int(width)
	}
	if height, ok := metadata["height"].(float64); ok {
		result["youtube_height"] = int(height)
	}
	if fps, ok := metadata["fps"].(float64); ok {
		result["youtube_fps"] = fps
	}
	if uploader, ok := metadata["uploader"].(string); ok {
		result["youtube_uploader"] = uploader
	}
	if uploadDate, ok := metadata["upload_date"].(string); ok {
		result["youtube_upload_date"] = uploadDate
	}
	if viewCount, ok := metadata["view_count"].(float64); ok {
		result["youtube_views"] = int64(viewCount)
	}

	return result, nil
}

// parseJSON is a helper to parse JSON with error handling
func parseJSON(data []byte, v interface{}) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("JSON unmarshal failed: %w", err)
	}
	return nil
}

// Cleanup removes downloaded files for a specific job
func (yd *YouTubeDownloader) Cleanup(jobID string) error {
	// Remove all files matching the job ID pattern
	pattern := filepath.Join(yd.outputDir, fmt.Sprintf("%s_youtube.*", jobID))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to find files to cleanup: %w", err)
	}

	for _, file := range matches {
		if err := os.Remove(file); err != nil {
			// Log error but continue cleanup
			fmt.Printf("Warning: failed to remove file %s: %v\n", file, err)
		}
	}

	return nil
}
