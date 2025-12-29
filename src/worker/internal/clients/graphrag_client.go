/**
 * GraphRAG Client for VideoAgent
 *
 * Provides embedding generation via GraphRAG service's centralized VoyageAI endpoint.
 * This avoids code duplication and ensures all services use the same embedding logic.
 */

package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// GraphRAGClient handles embedding generation via GraphRAG service

// GraphRAGClient handles embedding generation via GraphRAG service
type GraphRAGClient struct {
	baseURL    string
	httpClient *http.Client
}

// EmbeddingRequest represents the request to GraphRAG embedding API
type EmbeddingRequest struct {
	Content     string `json:"content"`
	InputType   string `json:"inputType"`
	ContentType string `json:"contentType"`
}

// EmbeddingResponse represents the response from GraphRAG
type EmbeddingResponse struct {
	Success    bool      `json:"success"`
	Embedding  []float64 `json:"embedding"`
	Dimensions int       `json:"dimensions"`
	Model      string    `json:"model"`
	Endpoint   string    `json:"endpoint"`
	Error      string    `json:"error,omitempty"`
	Details    string    `json:"details,omitempty"`
}

// NewGraphRAGClient creates a new GraphRAG client for embedding generation
func NewGraphRAGClient(baseURL string) (*GraphRAGClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("GraphRAG base URL is required")
	}

	return &GraphRAGClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // 30s timeout for embedding generation (typical: 100ms-2s)
		},
	}, nil
}

// GenerateEmbedding generates a VoyageAI embedding via GraphRAG service
//
// This is a SYNCHRONOUS operation with typical response time of 100ms - 2s.
// GraphRAG handles circuit breaking, retry logic, and VoyageAI API key management.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - text: Text content to generate embedding for
//   - inputType: "document" or "query" (default: "document")
//
// Returns:
//   - []float64: Embedding vector (1024 dimensions for voyage-3)
//   - error: Error if generation fails
func (g *GraphRAGClient) GenerateEmbedding(ctx context.Context, text string, inputType string) ([]float64, error) {
	if text == "" {
		return nil, fmt.Errorf("text is required for embedding generation")
	}

	// Prepare request
	reqBody := EmbeddingRequest{
		Content:     text,
		InputType:   inputType,
		ContentType: "general",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/api/embeddings/generate", g.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request (synchronous, waits for response)
	startTime := time.Now()
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GraphRAG request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response
	var embResp EmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse embedding response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK || !embResp.Success {
		return nil, fmt.Errorf("GraphRAG returned error (status %d): %s - %s",
			resp.StatusCode, embResp.Error, embResp.Details)
	}

	// Validate embedding
	if len(embResp.Embedding) == 0 {
		return nil, fmt.Errorf("received empty embedding from GraphRAG")
	}

	// Validate dimensions (voyage-3 returns 1024-D)
	if embResp.Dimensions != 1024 && embResp.Dimensions != 512 {
		log.Printf("Warning: Unexpected embedding dimensions: got %d, expected 1024 or 512", embResp.Dimensions)
	}

	// Log success
	log.Printf("Embedding generated via GraphRAG: dimensions=%d, model=%s, duration=%v",
		embResp.Dimensions, embResp.Model, duration)

	return embResp.Embedding, nil
}

// GenerateEmbeddingBatch generates embeddings for multiple texts via GraphRAG
//
// Currently processes sequentially. For production, consider implementing parallel requests
// with rate limiting.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - texts: Array of text content to generate embeddings for
//   - inputType: "document" or "query" for all texts
//
// Returns:
//   - [][]float64: Array of embedding vectors
//   - error: Error if any generation fails
func (g *GraphRAGClient) GenerateEmbeddingBatch(ctx context.Context, texts []string, inputType string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided for batch embedding")
	}

	embeddings := make([][]float64, len(texts))

	for i, text := range texts {
		embedding, err := g.GenerateEmbedding(ctx, text, inputType)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for text %d: %w", i, err)
		}
		embeddings[i] = embedding

		// Small delay between requests to avoid overwhelming GraphRAG
		if i < len(texts)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	log.Printf("Batch embedding generation completed: %d embeddings generated", len(embeddings))

	return embeddings, nil
}
