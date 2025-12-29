package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/utils"
)

// NexusAuthClient provides integration with Nexus-Auth service for OAuth tokens
type NexusAuthClient struct {
	baseURL           string
	internalServiceKey string
	httpClient        *http.Client
	serviceName       string
}

// NewNexusAuthClient creates a new Nexus-Auth client for internal service communication
func NewNexusAuthClient(baseURL, serviceKey string) (*NexusAuthClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("Nexus-Auth base URL is required")
	}

	if serviceKey == "" {
		return nil, fmt.Errorf("Internal service API key is required for Nexus-Auth communication")
	}

	return &NexusAuthClient{
		baseURL:           baseURL,
		internalServiceKey: serviceKey,
		serviceName:       "nexus-videoagent",
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // Quick timeout for token fetch
		},
	}, nil
}

// youtubeTokenInternal represents the token data returned from Nexus-Auth API
type youtubeTokenInternal struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scopes       []string  `json:"scopes"`
	ChannelID    string    `json:"channel_id"`
	IsValid      bool      `json:"is_valid"`
}

// YouTubeTokenResponse represents the internal API response
type YouTubeTokenResponse struct {
	Success bool                  `json:"success"`
	Token   *youtubeTokenInternal `json:"token,omitempty"`
	Error   string                `json:"error,omitempty"`
	Code    string                `json:"code,omitempty"` // Error code: "no_token", "expired", "revoked"
	Message string                `json:"message,omitempty"`
	AuthURL string                `json:"auth_url,omitempty"` // URL for user to authorize
}

// GetYouTubeToken fetches YouTube OAuth token for a user from Nexus-Auth
//
// This method calls the internal service API endpoint that:
// 1. Fetches the user's YouTube OAuth token from database
// 2. Decrypts the token
// 3. Auto-refreshes if expired
// 4. Returns ready-to-use access token
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - userID: User ID (UUID string)
//
// Returns:
//   - *utils.YouTubeToken: Valid, fresh access token if user has authorized YouTube
//   - error: Error with detailed message if token unavailable or fetch fails
//
// Error Codes (check error message or YouTubeTokenResponse.Code):
//   - "no_token": User has not authorized YouTube yet
//   - "expired": Token expired and auto-refresh failed
//   - "revoked": User revoked YouTube access
//   - "network_error": Failed to connect to Nexus-Auth
//   - "invalid_response": Nexus-Auth returned malformed response
func (c *NexusAuthClient) GetYouTubeToken(ctx context.Context, userID string) (*utils.YouTubeToken, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID is required for YouTube token fetch")
	}

	// Construct internal API endpoint
	// Format: GET /auth/internal/youtube/token/:userId
	endpoint := fmt.Sprintf("%s/auth/internal/youtube/token/%s", c.baseURL, userID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube token request: %w", err)
	}

	// Add internal service authentication headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service", c.serviceName)
	req.Header.Set("X-API-Key", c.internalServiceKey)
	req.Header.Set("X-Request-ID", fmt.Sprintf("videoagent-yt-%d", time.Now().UnixNano()))

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &YouTubeTokenError{
			Code:    "network_error",
			Message: fmt.Sprintf("Failed to connect to Nexus-Auth: %v", err),
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &YouTubeTokenError{
			Code:    "network_error",
			Message: fmt.Sprintf("Failed to read Nexus-Auth response: %v", err),
			Cause:   err,
		}
	}

	// Parse response
	var tokenResp YouTubeTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, &YouTubeTokenError{
			Code:    "invalid_response",
			Message: fmt.Sprintf("Failed to parse Nexus-Auth response: %v. Body: %s", err, string(body)),
			Cause:   err,
		}
	}

	// Check HTTP status code and response success
	if resp.StatusCode != http.StatusOK || !tokenResp.Success {
		// Map error codes to actionable errors
		switch tokenResp.Code {
		case "no_token":
			return nil, &YouTubeTokenError{
				Code:    "no_token",
				Message: fmt.Sprintf("User %s has not authorized YouTube access. Auth URL: %s", userID, tokenResp.AuthURL),
				AuthURL: tokenResp.AuthURL,
			}
		case "expired":
			return nil, &YouTubeTokenError{
				Code:    "expired",
				Message: fmt.Sprintf("YouTube token expired and auto-refresh failed for user %s. User must re-authorize.", userID),
				AuthURL: tokenResp.AuthURL,
			}
		case "revoked":
			return nil, &YouTubeTokenError{
				Code:    "revoked",
				Message: fmt.Sprintf("YouTube access revoked by user %s. User must re-authorize.", userID),
				AuthURL: tokenResp.AuthURL,
			}
		default:
			return nil, &YouTubeTokenError{
				Code:    tokenResp.Code,
				Message: fmt.Sprintf("Nexus-Auth error (status %d): %s", resp.StatusCode, tokenResp.Error),
			}
		}
	}

	// Validate token data
	if tokenResp.Token == nil {
		return nil, &YouTubeTokenError{
			Code:    "invalid_response",
			Message: "Nexus-Auth returned success=true but no token data",
		}
	}

	if tokenResp.Token.AccessToken == "" {
		return nil, &YouTubeTokenError{
			Code:    "invalid_response",
			Message: "Nexus-Auth returned empty access token",
		}
	}

	// Check token validity
	if !tokenResp.Token.IsValid {
		return nil, &YouTubeTokenError{
			Code:    "expired",
			Message: fmt.Sprintf("YouTube token is marked as invalid (expires at %v)", tokenResp.Token.ExpiresAt),
			AuthURL: tokenResp.AuthURL,
		}
	}

	// Convert internal token to utils.YouTubeToken
	return &utils.YouTubeToken{
		AccessToken: tokenResp.Token.AccessToken,
		ExpiresAt:   tokenResp.Token.ExpiresAt,
		Scopes:      tokenResp.Token.Scopes,
		ChannelID:   tokenResp.Token.ChannelID,
		IsValid:     tokenResp.Token.IsValid,
	}, nil
}

// RefreshYouTubeToken triggers a manual refresh of YouTube token
// (Normally auto-refresh happens in GetYouTubeToken, this is for explicit refresh)
func (c *NexusAuthClient) RefreshYouTubeToken(ctx context.Context, userID string) (*utils.YouTubeToken, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID is required for YouTube token refresh")
	}

	// Construct internal API endpoint
	// Format: POST /auth/internal/youtube/token/:userId/refresh
	endpoint := fmt.Sprintf("%s/auth/internal/youtube/token/%s/refresh", c.baseURL, userID)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube token refresh request: %w", err)
	}

	// Add internal service authentication headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service", c.serviceName)
	req.Header.Set("X-API-Key", c.internalServiceKey)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &YouTubeTokenError{
			Code:    "network_error",
			Message: fmt.Sprintf("Failed to connect to Nexus-Auth: %v", err),
			Cause:   err,
		}
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &YouTubeTokenError{
			Code:    "network_error",
			Message: fmt.Sprintf("Failed to read Nexus-Auth response: %v", err),
			Cause:   err,
		}
	}

	// Parse response
	var tokenResp YouTubeTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, &YouTubeTokenError{
			Code:    "invalid_response",
			Message: fmt.Sprintf("Failed to parse Nexus-Auth response: %v", err),
			Cause:   err,
		}
	}

	// Check success
	if resp.StatusCode != http.StatusOK || !tokenResp.Success {
		return nil, &YouTubeTokenError{
			Code:    tokenResp.Code,
			Message: fmt.Sprintf("Token refresh failed: %s", tokenResp.Error),
		}
	}

	if tokenResp.Token == nil {
		return nil, &YouTubeTokenError{
			Code:    "invalid_response",
			Message: "Nexus-Auth returned success=true but no token data",
		}
	}

	// Convert internal token to utils.YouTubeToken
	return &utils.YouTubeToken{
		AccessToken: tokenResp.Token.AccessToken,
		ExpiresAt:   tokenResp.Token.ExpiresAt,
		Scopes:      tokenResp.Token.Scopes,
		ChannelID:   tokenResp.Token.ChannelID,
		IsValid:     tokenResp.Token.IsValid,
	}, nil
}

// TrackYouTubeUsage reports YouTube API usage to Nexus-Auth for quota tracking
func (c *NexusAuthClient) TrackYouTubeUsage(ctx context.Context, userID string, quotaUnits int64) error {
	if userID == "" {
		return fmt.Errorf("user ID is required for usage tracking")
	}

	// Construct tracking payload
	payload := map[string]interface{}{
		"user_id":     userID,
		"service":     "youtube",
		"quota_units": quotaUnits,
		"timestamp":   time.Now().Unix(),
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal usage payload: %w", err)
	}

	// Construct endpoint
	endpoint := fmt.Sprintf("%s/internal/track-usage", c.baseURL)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create usage tracking request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service", c.serviceName)
	req.Header.Set("X-API-Key", c.internalServiceKey)

	// Send request (fire and forget - don't fail job if tracking fails)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: Failed to track YouTube usage: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Log warning but don't fail
		fmt.Printf("Warning: Usage tracking returned status %d\n", resp.StatusCode)
	}

	return nil
}

// HealthCheck verifies Nexus-Auth service is reachable
func (c *NexusAuthClient) HealthCheck(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/health", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Nexus-Auth health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Nexus-Auth unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// YouTubeTokenError represents a YouTube token fetch error with actionable information
type YouTubeTokenError struct {
	Code    string // Error code: "no_token", "expired", "revoked", "network_error", "invalid_response"
	Message string // Human-readable error message
	AuthURL string // URL for user to authorize YouTube (if applicable)
	Cause   error  // Underlying error (if applicable)
}

func (e *YouTubeTokenError) Error() string {
	return e.Message
}

// IsNoToken returns true if error is due to user not having authorized YouTube
func (e *YouTubeTokenError) IsNoToken() bool {
	return e.Code == "no_token"
}

// IsExpired returns true if error is due to expired token
func (e *YouTubeTokenError) IsExpired() bool {
	return e.Code == "expired"
}

// IsRevoked returns true if error is due to revoked authorization
func (e *YouTubeTokenError) IsRevoked() bool {
	return e.Code == "revoked"
}

// IsNetworkError returns true if error is due to network/connectivity issues
func (e *YouTubeTokenError) IsNetworkError() bool {
	return e.Code == "network_error"
}

// RequiresUserAction returns true if error requires user to re-authorize
func (e *YouTubeTokenError) RequiresUserAction() bool {
	return e.IsNoToken() || e.IsExpired() || e.IsRevoked()
}
