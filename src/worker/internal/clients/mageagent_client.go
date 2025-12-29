package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// MageAgentClient provides integration with MageAgent service (zero hardcoded models)
type MageAgentClient struct {
	baseURL    string
	httpClient *http.Client
	retryCount int
	timeout    time.Duration
}

// NewMageAgentClient creates a new MageAgent client
func NewMageAgentClient(baseURL string, timeout time.Duration) *MageAgentClient {
	return &MageAgentClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retryCount: 3,
		timeout:    timeout,
	}
}

// SelectModel dynamically selects the best model for a task (ZERO HARDCODED MODELS)
// This is the core of the zero-hardcoding architecture
func (c *MageAgentClient) SelectModel(ctx context.Context, req models.MageAgentModelRequest) (*models.MageAgentModelResponse, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/models/select", c.baseURL)

	payload := map[string]interface{}{
		"taskType":   req.TaskType,
		"complexity": req.Complexity,
		"context":    req.Context,
	}

	if req.Budget > 0 {
		payload["budget"] = req.Budget
	}

	var response models.MageAgentModelResponse
	if err := c.makeRequest(ctx, "POST", endpoint, payload, &response); err != nil {
		return nil, fmt.Errorf("model selection failed: %w", err)
	}

	return &response, nil
}

// AnalyzeFrame analyzes a video frame using vision model (async-first with polling)
func (c *MageAgentClient) AnalyzeFrame(ctx context.Context, req models.MageAgentVisionRequest) (*models.MageAgentVisionResponse, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/vision/analyze", c.baseURL)

	payload := map[string]interface{}{
		"image":              req.Image, // Base64-encoded
		"prompt":             req.Prompt,
		"modelId":            req.ModelID, // From SelectModel
		"maxTokens":          req.MaxTokens,
		"additionalContext":  req.AdditionalContext,
	}

	// Submit task and poll until completion (async-first pattern from Phase 1)
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return nil, fmt.Errorf("frame analysis failed: %w", err)
	}

	// Parse result into Raw VisionResponse (handles both string and array formats)
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var rawResponse models.MageAgentVisionResponseRaw
	if err := json.Unmarshal(jsonData, &rawResponse); err != nil {
		return nil, fmt.Errorf("failed to parse vision response: %w", err)
	}

	// Normalize response (converts text descriptions to structured objects)
	response, err := rawResponse.NormalizeResponse()
	if err != nil {
		return nil, fmt.Errorf("failed to normalize vision response: %w", err)
	}

	return response, nil
}

// TranscribeAudio transcribes audio using speech-to-text model (async-first with polling)
func (c *MageAgentClient) TranscribeAudio(ctx context.Context, req models.MageAgentTranscriptionRequest) (*models.MageAgentTranscriptionResponse, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/audio/transcribe", c.baseURL)

	payload := map[string]interface{}{
		"audio":             req.Audio, // Base64-encoded WAV
		"language":          req.Language, // "auto" for detection
		"modelId":           req.ModelID,
		"enableDiarization": req.EnableDiarization,
	}

	if req.Context != "" {
		payload["context"] = req.Context
	}

	// Submit task and poll until completion
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return nil, fmt.Errorf("transcription failed: %w", err)
	}

	// Parse result into TranscriptionResponse
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var response models.MageAgentTranscriptionResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse transcription response: %w", err)
	}

	return &response, nil
}

// Synthesize synthesizes information from multiple sources (async-first with polling)
func (c *MageAgentClient) Synthesize(ctx context.Context, sources []string, format string, objective string) (string, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/synthesize", c.baseURL)

	payload := map[string]interface{}{
		"sources": sources,
		"format":  format,
	}

	if objective != "" {
		payload["objective"] = objective
	}

	// Submit task and poll until completion
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return "", fmt.Errorf("synthesis failed: %w", err)
	}

	// Parse result
	jsonData, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return "", fmt.Errorf("failed to parse synthesis response: %w", err)
	}

	// Extract synthesized result
	if resultStr, ok := response["result"].(string); ok {
		return resultStr, nil
	}

	return "", fmt.Errorf("unexpected synthesis response format")
}

// Orchestrate orchestrates multiple AI agents for complex tasks (async-first with polling)
func (c *MageAgentClient) Orchestrate(ctx context.Context, task string, maxAgents int, contextData map[string]interface{}) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/orchestrate", c.baseURL)

	payload := map[string]interface{}{
		"task":      task,
		"maxAgents": maxAgents,
		"context":   contextData,
	}

	// Submit task and poll until completion (use longer timeout for orchestration)
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, 300*time.Second) // 5 minutes for orchestration
	if err != nil {
		return nil, fmt.Errorf("orchestration failed: %w", err)
	}

	// Parse result
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse orchestration response: %w", err)
	}

	return response, nil
}

// ClassifyContent classifies video content using AI (async-first with polling)
func (c *MageAgentClient) ClassifyContent(ctx context.Context, description string, frames []string, modelID string) (*models.ContentClassification, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/text/classify", c.baseURL)

	payload := map[string]interface{}{
		"content": description,
		"frames":  frames, // Sample frame descriptions
		"modelId": modelID,
	}

	// Submit task and poll until completion (async-first pattern from Phase 1)
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return nil, fmt.Errorf("classification failed: %w", err)
	}

	// Parse result into ContentClassification
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var response models.ContentClassification
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse classification response: %w", err)
	}

	return &response, nil
}

// ExtractTopics extracts topics from transcription (async-first with polling)
func (c *MageAgentClient) ExtractTopics(ctx context.Context, text string, modelID string) ([]string, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/text/topics", c.baseURL)

	payload := map[string]interface{}{
		"text":       text,
		"max_topics": 10,
	}

	// Submit task and poll until completion
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return nil, fmt.Errorf("topic extraction failed: %w", err)
	}

	// Parse result into topics array
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var response struct {
		Topics []string `json:"topics"`
	}

	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse topics response: %w", err)
	}

	return response.Topics, nil
}

// AnalyzeSentiment analyzes sentiment of text (async-first with polling)
func (c *MageAgentClient) AnalyzeSentiment(ctx context.Context, text string, modelID string) (string, float64, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/text/sentiment", c.baseURL)

	payload := map[string]interface{}{
		"text": text,
	}

	// Submit task and poll until completion
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return "", 0, fmt.Errorf("sentiment analysis failed: %w", err)
	}

	// Parse result into sentiment response
	jsonData, err := json.Marshal(result)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal result: %w", err)
	}

	var response struct {
		Sentiment  string  `json:"sentiment"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal(jsonData, &response); err != nil {
		return "", 0, fmt.Errorf("failed to parse sentiment response: %w", err)
	}

	return response.Sentiment, response.Confidence, nil
}

// GenerateEmbedding generates vector embedding for semantic search (async-first with polling)
func (c *MageAgentClient) GenerateEmbedding(ctx context.Context, text string, modelID string) ([]float32, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/embedding/generate", c.baseURL)

	payload := map[string]interface{}{
		"text": text,
	}

	// Submit task and poll until completion
	result, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return nil, fmt.Errorf("embedding generation failed: %w", err)
	}

	// Parse result into embedding array
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var response struct {
		Embedding []float32 `json:"embedding"`
	}

	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to parse embedding response: %w", err)
	}

	return response.Embedding, nil
}

// StoreMemory stores memory in MageAgent (async-first with polling)
func (c *MageAgentClient) StoreMemory(ctx context.Context, content string, tags []string, metadata map[string]interface{}) error {
	endpoint := fmt.Sprintf("%s/mageagent/api/memory/store", c.baseURL)

	payload := map[string]interface{}{
		"content":  content,
		"tags":     tags,
		"metadata": metadata,
	}

	// Submit task and poll until completion
	_, err := c.submitTaskAndPoll(ctx, endpoint, payload, DefaultTaskTimeout)
	if err != nil {
		return fmt.Errorf("memory storage failed: %w", err)
	}

	return nil
}

// TrackModelUsage tracks model usage for learning and optimization (async-first with polling)
// NOTE: This endpoint does not exist in MageAgent yet. Needs implementation.
// TODO: Add /models/track-usage endpoint to MageAgent or use alternative tracking mechanism
func (c *MageAgentClient) TrackModelUsage(ctx context.Context, usage models.ModelUsageRecord) error {
	// Temporarily disabled - endpoint doesn't exist yet
	// endpoint := fmt.Sprintf("%s/models/track-usage", c.baseURL)

	// For now, just log that usage tracking was called
	// In future, this should call the proper endpoint when implemented
	return nil // No-op until endpoint is implemented
}

// Async polling configuration constants
const (
	DefaultPollInterval = 2 * time.Second
	DefaultTaskTimeout  = 120 * time.Second
	MaxPollAttempts     = 60
)

// submitTaskAndPoll submits a task to MageAgent and polls until completion
// This implements the async-first pattern from Phase 1
func (c *MageAgentClient) submitTaskAndPoll(ctx context.Context, endpoint string, payload interface{}, timeout time.Duration) (interface{}, error) {
	// Step 1: Submit task and get taskId (202 Accepted)
	var taskResp models.TaskSubmitResponse
	if err := c.makeRequest(ctx, "POST", endpoint, payload, &taskResp); err != nil {
		return nil, fmt.Errorf("task submission failed: %w", err)
	}

	if !taskResp.Success {
		return nil, fmt.Errorf("task submission returned success=false: %s", taskResp.Message)
	}

	// Step 2: Poll until task completes
	result, err := c.waitForTaskCompletion(ctx, taskResp.TaskID, DefaultPollInterval, timeout)
	if err != nil {
		return nil, fmt.Errorf("task polling failed: %w", err)
	}

	return result, nil
}

// pollTaskStatus queries MageAgent for the current status of a task.
//
// This method implements defensive parsing of MageAgent's nested response structure:
//
//	{success: true, data: {task: {id, status, result, ...}}}
//
// Returns:
//   - *models.TaskDetails: Validated task details with guaranteed non-null fields
//   - error: Detailed error if polling failed or response is malformed
func (c *MageAgentClient) pollTaskStatus(ctx context.Context, taskID string) (*models.TaskDetails, error) {
	endpoint := fmt.Sprintf("%s/mageagent/api/tasks/%s", c.baseURL, taskID)

	// Parse into nested response structure
	var statusResp models.MageAgentTaskStatusResponse
	if err := c.makeRequest(ctx, "GET", endpoint, nil, &statusResp); err != nil {
		return nil, fmt.Errorf("failed to poll task status: %w", err)
	}

	// Defensive validation and extraction
	taskDetails, err := statusResp.ValidateAndExtract()
	if err != nil {
		return nil, fmt.Errorf(
			"invalid response from MageAgent task status endpoint %s: %w. "+
				"This indicates either a breaking change in MageAgent API or a network/parsing error. "+
				"Response received: success=%v, has_data=%v",
			endpoint, err, statusResp.Success, statusResp.Data != nil,
		)
	}

	return taskDetails, nil
}

// waitForTaskCompletion polls MageAgent until the task reaches a terminal state.
//
// Polling Strategy:
//   - Poll every 2 seconds (DefaultPollInterval)
//   - Timeout after 120 seconds (DefaultTaskTimeout)
//   - Max 60 attempts (MaxPollAttempts)
//
// Terminal States:
//   - "completed": Task finished successfully
//   - "failed": Task failed with error
//   - "timeout": Task exceeded processing timeout
//
// Returns:
//   - interface{}: Task result if completed successfully
//   - error: Detailed error if task failed or polling timed out
func (c *MageAgentClient) waitForTaskCompletion(
	ctx context.Context,
	taskID string,
	pollInterval time.Duration,
	timeout time.Duration,
) (interface{}, error) {
	if pollInterval == 0 {
		pollInterval = DefaultPollInterval
	}
	if timeout == 0 {
		timeout = DefaultTaskTimeout
	}

	// Create timeout context
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Polling loop with exponential backoff for transient errors
	attempt := 0
	consecutiveErrors := 0
	maxConsecutiveErrors := 5

	for {
		attempt++

		// Check if context cancelled or timed out
		select {
		case <-pollCtx.Done():
			if pollCtx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf(
					"task %s polling timed out after %v (attempted %d polls). "+
						"The task may still be running in MageAgent. "+
						"Consider increasing timeout or checking MageAgent logs for task status. "+
						"Last known state: unknown (polling stopped before terminal state reached)",
					taskID, timeout, attempt,
				)
			}
			return nil, fmt.Errorf("task %s polling cancelled: %w", taskID, pollCtx.Err())
		default:
			// Continue polling
		}

		// Enforce max attempts as safety limit
		if attempt > MaxPollAttempts {
			return nil, fmt.Errorf(
				"task %s exceeded maximum poll attempts (%d attempts over %v). "+
					"The task may be stuck in MageAgent. Check MageAgent logs for task %s.",
				taskID, MaxPollAttempts, timeout, taskID,
			)
		}

		// Poll task status with defensive error handling
		taskDetails, err := c.pollTaskStatus(pollCtx, taskID)
		if err != nil {
			consecutiveErrors++

			// Fail fast if too many consecutive errors (indicates systemic issue)
			if consecutiveErrors >= maxConsecutiveErrors {
				return nil, fmt.Errorf(
					"task %s polling failed after %d consecutive errors. "+
						"This indicates MageAgent is unavailable or returning malformed responses. "+
						"Last error: %w. "+
						"Check MageAgent health and network connectivity.",
					taskID, consecutiveErrors, err,
				)
			}

			// Transient error - retry after interval
			select {
			case <-pollCtx.Done():
				return nil, fmt.Errorf("task %s polling cancelled while handling error: %w", taskID, err)
			case <-time.After(pollInterval):
				continue
			}
		}

		// Reset consecutive error counter on successful poll
		consecutiveErrors = 0

		// Check if task reached terminal state
		if taskDetails.IsTerminal() {
			if taskDetails.IsSuccessful() {
				// Task completed successfully
				if taskDetails.Result == nil {
					return nil, fmt.Errorf(
						"task %s completed successfully but returned no result data. "+
							"This indicates MageAgent processor for task type '%s' returned nil/undefined. "+
							"Check MageAgent processor implementation for type: %s",
						taskID, taskDetails.Type, taskDetails.Type,
					)
				}
				return taskDetails.Result, nil
			} else {
				// Task failed or timed out
				errorMsg := taskDetails.GetErrorMessage()
				return nil, fmt.Errorf(
					"task %s failed with status '%s': %s. "+
						"Task type: %s. "+
						"Started: %v, Completed: %v. "+
						"Check MageAgent logs for detailed error trace.",
					taskID, taskDetails.Status, errorMsg, taskDetails.Type,
					taskDetails.StartedAt, taskDetails.CompletedAt,
				)
			}
		}

		// Task still in progress - wait before next poll
		select {
		case <-pollCtx.Done():
			return nil, fmt.Errorf("task %s polling cancelled while waiting for next poll", taskID)
		case <-time.After(pollInterval):
			continue
		}
	}
}

// makeRequest is a generic HTTP request helper with retry logic
func (c *MageAgentClient) makeRequest(ctx context.Context, method, url string, payload interface{}, result interface{}) error {
	var lastErr error

	for attempt := 0; attempt <= c.retryCount; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt*attempt) * time.Second
			time.Sleep(backoff)
		}

		err := c.doRequest(ctx, method, url, payload, result)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !c.isRetryable(err) {
			return err
		}
	}

	return fmt.Errorf("request failed after %d attempts: %w", c.retryCount+1, lastErr)
}

// doRequest performs a single HTTP request
func (c *MageAgentClient) doRequest(ctx context.Context, method, url string, payload interface{}, result interface{}) error {
	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", fmt.Sprintf("videoagent-%d", time.Now().UnixNano()))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code - accept both 200 OK (sync) and 202 Accepted (async)
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return nil
}

// isRetryable determines if an error is retryable
func (c *MageAgentClient) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Retry on timeout and temporary errors
	errStr := err.Error()
	return bytes.Contains([]byte(errStr), []byte("timeout")) ||
		bytes.Contains([]byte(errStr), []byte("temporary")) ||
		bytes.Contains([]byte(errStr), []byte("connection refused")) ||
		bytes.Contains([]byte(errStr), []byte("429")) // Rate limit
}

// HealthCheck checks if MageAgent service is available
func (c *MageAgentClient) HealthCheck(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/health", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MageAgent unhealthy: status %d", resp.StatusCode)
	}

	return nil
}
