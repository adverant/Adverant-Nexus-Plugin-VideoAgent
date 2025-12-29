package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// YouTubeAPIClient uses YouTube Data API v3 for metadata extraction
// This works without proxy since it's an official API, not web scraping
type YouTubeAPIClient struct {
	apiKey     string
	httpClient *http.Client
}

// YouTubeVideoMetadata contains video information from YouTube Data API
type YouTubeVideoMetadata struct {
	VideoID          string    `json:"video_id"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	ChannelID        string    `json:"channel_id"`
	ChannelTitle     string    `json:"channel_title"`
	PublishedAt      time.Time `json:"published_at"`
	Duration         string    `json:"duration"`          // ISO 8601 duration (PT4M13S)
	DurationSeconds  int       `json:"duration_seconds"`  // Parsed duration in seconds
	ViewCount        int64     `json:"view_count"`
	LikeCount        int64     `json:"like_count"`
	CommentCount     int64     `json:"comment_count"`
	ThumbnailURL     string    `json:"thumbnail_url"`
	Tags             []string  `json:"tags"`
	CategoryID       string    `json:"category_id"`
	LiveBroadcast    string    `json:"live_broadcast"`    // none, live, upcoming
	DefaultLanguage  string    `json:"default_language"`
	DefaultAudioLang string    `json:"default_audio_language"`
	Definition       string    `json:"definition"`        // hd or sd
	Dimension        string    `json:"dimension"`         // 2d or 3d
	Caption          string    `json:"caption"`           // true or false (as string)
	LicensedContent  bool      `json:"licensed_content"`
	// Location info (if available)
	LocationDescription string  `json:"location_description,omitempty"`
	Latitude            float64 `json:"latitude,omitempty"`
	Longitude           float64 `json:"longitude,omitempty"`
}

// YouTube Data API v3 response structures
type youtubeAPIResponse struct {
	Items []youtubeVideoItem `json:"items"`
}

type youtubeVideoItem struct {
	ID      string `json:"id"`
	Snippet struct {
		Title            string    `json:"title"`
		Description      string    `json:"description"`
		ChannelID        string    `json:"channelId"`
		ChannelTitle     string    `json:"channelTitle"`
		PublishedAt      time.Time `json:"publishedAt"`
		Tags             []string  `json:"tags"`
		CategoryID       string    `json:"categoryId"`
		LiveBroadcastContent string `json:"liveBroadcastContent"`
		DefaultLanguage  string    `json:"defaultLanguage"`
		DefaultAudioLanguage string `json:"defaultAudioLanguage"`
		Thumbnails       struct {
			MaxRes struct {
				URL string `json:"url"`
			} `json:"maxres"`
			High struct {
				URL string `json:"url"`
			} `json:"high"`
			Medium struct {
				URL string `json:"url"`
			} `json:"medium"`
		} `json:"thumbnails"`
	} `json:"snippet"`
	ContentDetails struct {
		Duration        string `json:"duration"`
		Dimension       string `json:"dimension"`
		Definition      string `json:"definition"`
		Caption         string `json:"caption"`
		LicensedContent bool   `json:"licensedContent"`
	} `json:"contentDetails"`
	Statistics struct {
		ViewCount    string `json:"viewCount"`
		LikeCount    string `json:"likeCount"`
		CommentCount string `json:"commentCount"`
	} `json:"statistics"`
	RecordingDetails struct {
		LocationDescription string `json:"locationDescription"`
		Location            struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"location"`
	} `json:"recordingDetails"`
}

// NewYouTubeAPIClient creates a new YouTube Data API client
// Environment variable: YOUTUBE_API_KEY
func NewYouTubeAPIClient() (*YouTubeAPIClient, error) {
	apiKey := os.Getenv("YOUTUBE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("YOUTUBE_API_KEY environment variable not set")
	}

	// Validate API key format (basic check)
	if len(apiKey) < 30 {
		return nil, fmt.Errorf("YOUTUBE_API_KEY appears invalid (too short)")
	}

	log.Printf("[YouTubeAPIClient] Initialized with API key: %s...%s",
		apiKey[:8], apiKey[len(apiKey)-4:])

	return &YouTubeAPIClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// ExtractVideoID extracts the video ID from various YouTube URL formats
func ExtractVideoID(youtubeURL string) (string, error) {
	// Handle youtu.be short URLs
	if strings.Contains(youtubeURL, "youtu.be/") {
		parts := strings.Split(youtubeURL, "youtu.be/")
		if len(parts) >= 2 {
			videoID := strings.Split(parts[1], "?")[0]
			videoID = strings.Split(videoID, "&")[0]
			if len(videoID) == 11 {
				return videoID, nil
			}
		}
	}

	// Handle youtube.com URLs with v= parameter
	if strings.Contains(youtubeURL, "v=") {
		u, err := url.Parse(youtubeURL)
		if err == nil {
			videoID := u.Query().Get("v")
			if len(videoID) == 11 {
				return videoID, nil
			}
		}
	}

	// Handle youtube.com/embed/ URLs
	if strings.Contains(youtubeURL, "/embed/") {
		re := regexp.MustCompile(`/embed/([a-zA-Z0-9_-]{11})`)
		matches := re.FindStringSubmatch(youtubeURL)
		if len(matches) >= 2 {
			return matches[1], nil
		}
	}

	// Handle youtube.com/v/ URLs
	if strings.Contains(youtubeURL, "/v/") {
		re := regexp.MustCompile(`/v/([a-zA-Z0-9_-]{11})`)
		matches := re.FindStringSubmatch(youtubeURL)
		if len(matches) >= 2 {
			return matches[1], nil
		}
	}

	// Direct video ID (11 characters)
	if len(youtubeURL) == 11 && regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`).MatchString(youtubeURL) {
		return youtubeURL, nil
	}

	return "", fmt.Errorf("could not extract video ID from URL: %s", youtubeURL)
}

// GetVideoMetadata fetches video metadata using YouTube Data API v3
// This does NOT require a proxy since it uses official API
func (c *YouTubeAPIClient) GetVideoMetadata(ctx context.Context, videoURL string) (*YouTubeVideoMetadata, error) {
	videoID, err := ExtractVideoID(videoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract video ID: %w", err)
	}

	log.Printf("[YouTubeAPIClient] Fetching metadata for video ID: %s", videoID)

	// Build API URL
	apiURL := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/videos?part=snippet,contentDetails,statistics,recordingDetails&id=%s&key=%s",
		videoID,
		c.apiKey,
	)

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create API request: %w", err)
	}

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("YouTube API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("YouTube API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read API response: %w", err)
	}

	var apiResp youtubeAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	if len(apiResp.Items) == 0 {
		return nil, fmt.Errorf("video not found: %s", videoID)
	}

	item := apiResp.Items[0]

	// Build metadata struct
	metadata := &YouTubeVideoMetadata{
		VideoID:          item.ID,
		Title:            item.Snippet.Title,
		Description:      item.Snippet.Description,
		ChannelID:        item.Snippet.ChannelID,
		ChannelTitle:     item.Snippet.ChannelTitle,
		PublishedAt:      item.Snippet.PublishedAt,
		Duration:         item.ContentDetails.Duration,
		DurationSeconds:  parseISO8601Duration(item.ContentDetails.Duration),
		Tags:             item.Snippet.Tags,
		CategoryID:       item.Snippet.CategoryID,
		LiveBroadcast:    item.Snippet.LiveBroadcastContent,
		DefaultLanguage:  item.Snippet.DefaultLanguage,
		DefaultAudioLang: item.Snippet.DefaultAudioLanguage,
		Definition:       item.ContentDetails.Definition,
		Dimension:        item.ContentDetails.Dimension,
		Caption:          item.ContentDetails.Caption,
		LicensedContent:  item.ContentDetails.LicensedContent,
	}

	// Parse statistics (they come as strings)
	if item.Statistics.ViewCount != "" {
		fmt.Sscanf(item.Statistics.ViewCount, "%d", &metadata.ViewCount)
	}
	if item.Statistics.LikeCount != "" {
		fmt.Sscanf(item.Statistics.LikeCount, "%d", &metadata.LikeCount)
	}
	if item.Statistics.CommentCount != "" {
		fmt.Sscanf(item.Statistics.CommentCount, "%d", &metadata.CommentCount)
	}

	// Best thumbnail URL
	if item.Snippet.Thumbnails.MaxRes.URL != "" {
		metadata.ThumbnailURL = item.Snippet.Thumbnails.MaxRes.URL
	} else if item.Snippet.Thumbnails.High.URL != "" {
		metadata.ThumbnailURL = item.Snippet.Thumbnails.High.URL
	} else {
		metadata.ThumbnailURL = item.Snippet.Thumbnails.Medium.URL
	}

	// Location data (if available)
	if item.RecordingDetails.LocationDescription != "" {
		metadata.LocationDescription = item.RecordingDetails.LocationDescription
		metadata.Latitude = item.RecordingDetails.Location.Latitude
		metadata.Longitude = item.RecordingDetails.Location.Longitude
	}

	log.Printf("[YouTubeAPIClient] Successfully fetched metadata: %s (%d seconds)",
		metadata.Title, metadata.DurationSeconds)

	return metadata, nil
}

// ToMap converts YouTubeVideoMetadata to a map for compatibility
func (m *YouTubeVideoMetadata) ToMap() map[string]interface{} {
	result := map[string]interface{}{
		"youtube_video_id":       m.VideoID,
		"youtube_title":          m.Title,
		"youtube_description":    m.Description,
		"youtube_channel_id":     m.ChannelID,
		"youtube_channel":        m.ChannelTitle,
		"youtube_published_at":   m.PublishedAt,
		"youtube_duration":       m.DurationSeconds,
		"youtube_duration_iso":   m.Duration,
		"youtube_views":          m.ViewCount,
		"youtube_likes":          m.LikeCount,
		"youtube_comments":       m.CommentCount,
		"youtube_thumbnail":      m.ThumbnailURL,
		"youtube_tags":           m.Tags,
		"youtube_category_id":    m.CategoryID,
		"youtube_definition":     m.Definition,
		"youtube_has_captions":   m.Caption == "true",
		"youtube_licensed":       m.LicensedContent,
	}

	// Add location if available
	if m.LocationDescription != "" {
		result["youtube_location"] = m.LocationDescription
		result["youtube_latitude"] = m.Latitude
		result["youtube_longitude"] = m.Longitude
	}

	return result
}

// parseISO8601Duration parses duration like "PT4M13S" to seconds
func parseISO8601Duration(duration string) int {
	if duration == "" || !strings.HasPrefix(duration, "PT") {
		return 0
	}

	duration = strings.TrimPrefix(duration, "PT")
	var hours, minutes, seconds int

	// Parse hours
	if idx := strings.Index(duration, "H"); idx != -1 {
		fmt.Sscanf(duration[:idx], "%d", &hours)
		duration = duration[idx+1:]
	}

	// Parse minutes
	if idx := strings.Index(duration, "M"); idx != -1 {
		fmt.Sscanf(duration[:idx], "%d", &minutes)
		duration = duration[idx+1:]
	}

	// Parse seconds
	if idx := strings.Index(duration, "S"); idx != -1 {
		fmt.Sscanf(duration[:idx], "%d", &seconds)
	}

	return hours*3600 + minutes*60 + seconds
}
