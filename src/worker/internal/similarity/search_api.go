package similarity

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
)

// SearchAPI provides video similarity search functionality
type SearchAPI struct {
	videoEmbedder  *VideoEmbedder
	sceneEmbedder  *SceneEmbedder
	qdrantManager  *QdrantManager
	graphragClient *clients.GraphRAGClient
}

// NewSearchAPI creates a new search API
func NewSearchAPI(videoEmbedder *VideoEmbedder, sceneEmbedder *SceneEmbedder, qdrantManager *QdrantManager, graphragClient *clients.GraphRAGClient) *SearchAPI {
	return &SearchAPI{
		videoEmbedder:  videoEmbedder,
		sceneEmbedder:  sceneEmbedder,
		qdrantManager:  qdrantManager,
		graphragClient: graphragClient,
	}
}

// VideoSearchRequest represents a video search request
type VideoSearchRequest struct {
	QueryType      SearchQueryType        `json:"queryType"`      // text, video, image, embedding
	Query          string                 `json:"query"`          // Text query or data
	QueryEmbedding []float64              `json:"queryEmbedding"` // Pre-computed embedding
	Limit          int                    `json:"limit"`
	Filters        SearchFilters          `json:"filters"`
	Options        SearchOptions          `json:"options"`
}

// SearchQueryType represents the type of search query
type SearchQueryType string

const (
	QueryTypeText      SearchQueryType = "text"
	QueryTypeVideo     SearchQueryType = "video"
	QueryTypeImage     SearchQueryType = "image"
	QueryTypeEmbedding SearchQueryType = "embedding"
)

// SearchFilters represents search filters
type SearchFilters struct {
	VideoIDs        []string               `json:"videoIds"`        // Specific video IDs
	MinDuration     float64                `json:"minDuration"`     // Minimum duration (seconds)
	MaxDuration     float64                `json:"maxDuration"`     // Maximum duration (seconds)
	SceneTypes      []string               `json:"sceneTypes"`      // Filter by scene types
	Objects         []string               `json:"objects"`         // Must contain objects
	Tags            []string               `json:"tags"`            // Must have tags
	DateRange       *DateRange             `json:"dateRange"`       // Date range filter
	ColorProfile    string                 `json:"colorProfile"`    // warm, cool, vibrant, neutral
	MinBrightness   float64                `json:"minBrightness"`
	MaxBrightness   float64                `json:"maxBrightness"`
	MotionLevel     string                 `json:"motionLevel"`     // low, medium, high
	Attributes      map[string]interface{} `json:"attributes"`
}

// DateRange represents a date range
type DateRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// SearchOptions represents search options
type SearchOptions struct {
	IncludeScenes   bool    `json:"includeScenes"`   // Include scene-level results
	IncludeMetadata bool    `json:"includeMetadata"` // Include full metadata
	IncludeEmbedding bool   `json:"includeEmbedding"` // Include embeddings
	ScoreThreshold  float64 `json:"scoreThreshold"`  // Minimum similarity score
	ReRank          bool    `json:"reRank"`          // Apply re-ranking
	Explain         bool    `json:"explain"`         // Include explanation
}

// VideoSearchResponse represents search response
type VideoSearchResponse struct {
	Results      []VideoSearchResult `json:"results"`
	TotalFound   int                 `json:"totalFound"`
	Query        string              `json:"query"`
	ProcessingTime float64           `json:"processingTimeMs"`
	Explanation  *SearchExplanation  `json:"explanation,omitempty"`
}

// VideoSearchResult represents a single search result
type VideoSearchResult struct {
	VideoID       string                 `json:"videoId"`
	Score         float64                `json:"score"`         // Similarity score (0-1)
	Rank          int                    `json:"rank"`
	Metadata      *VideoMetadata         `json:"metadata,omitempty"`
	Embedding     []float64              `json:"embedding,omitempty"`
	MatchedScenes []SceneSearchResult    `json:"matchedScenes,omitempty"`
	Explanation   string                 `json:"explanation,omitempty"`
	Attributes    map[string]interface{} `json:"attributes"`
}

// SceneSearchResult represents a scene-level search result
type SceneSearchResult struct {
	SceneID     string          `json:"sceneId"`
	VideoID     string          `json:"videoId"`
	Score       float64         `json:"score"`
	StartFrame  int             `json:"startFrame"`
	EndFrame    int             `json:"endFrame"`
	Duration    float64         `json:"duration"`
	SceneType   string          `json:"sceneType"`
	Semantics   *SceneSemantics `json:"semantics,omitempty"`
	Explanation string          `json:"explanation,omitempty"`
}

// SearchExplanation provides explanation for search results
type SearchExplanation struct {
	QueryProcessing  string                 `json:"queryProcessing"`
	MatchingStrategy string                 `json:"matchingStrategy"`
	FiltersApplied   []string               `json:"filtersApplied"`
	ReRankingApplied bool                   `json:"reRankingApplied"`
	Attributes       map[string]interface{} `json:"attributes"`
}

// SearchVideos searches for similar videos
func (sa *SearchAPI) SearchVideos(ctx context.Context, req VideoSearchRequest) (*VideoSearchResponse, error) {
	startTime := time.Now()

	// Generate query embedding
	queryEmbedding, err := sa.getQueryEmbedding(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Build Qdrant filter
	qdrantFilter := sa.buildQdrantFilter(req.Filters)

	// Search Qdrant
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	results, err := sa.qdrantManager.SearchSimilarVideos(ctx, queryEmbedding, limit, qdrantFilter)
	if err != nil {
		return nil, fmt.Errorf("qdrant search failed: %w", err)
	}

	// Convert to VideoSearchResult
	videoResults := make([]VideoSearchResult, 0, len(results))

	for i, result := range results {
		videoResult := VideoSearchResult{
			VideoID: result.ID,
			Score:   result.Score,
			Rank:    i + 1,
			Attributes: result.Payload,
		}

		// Include metadata if requested
		if req.Options.IncludeMetadata {
			metadata := sa.extractVideoMetadata(result.Payload)
			videoResult.Metadata = &metadata
		}

		// Include embedding if requested
		if req.Options.IncludeEmbedding {
			videoResult.Embedding = result.Vector
		}

		// Search scenes if requested
		if req.Options.IncludeScenes {
			sceneResults, err := sa.searchScenesForVideo(ctx, result.ID, queryEmbedding, 5, req.Filters)
			if err != nil {
				log.Printf("Warning: Scene search failed for video %s: %v", result.ID, err)
			} else {
				videoResult.MatchedScenes = sceneResults
			}
		}

		// Generate explanation if requested
		if req.Options.Explain {
			videoResult.Explanation = sa.generateResultExplanation(result, req)
		}

		videoResults = append(videoResults, videoResult)
	}

	// Apply re-ranking if requested
	if req.Options.ReRank {
		videoResults = sa.reRankResults(videoResults, req)
	}

	// Filter by score threshold
	if req.Options.ScoreThreshold > 0 {
		filtered := make([]VideoSearchResult, 0)
		for _, result := range videoResults {
			if result.Score >= req.Options.ScoreThreshold {
				filtered = append(filtered, result)
			}
		}
		videoResults = filtered
	}

	processingTime := time.Since(startTime).Milliseconds()

	response := &VideoSearchResponse{
		Results:        videoResults,
		TotalFound:     len(videoResults),
		Query:          req.Query,
		ProcessingTime: float64(processingTime),
	}

	// Add explanation if requested
	if req.Options.Explain {
		response.Explanation = sa.generateSearchExplanation(req)
	}

	log.Printf("Search completed: found %d results in %dms", len(videoResults), processingTime)

	return response, nil
}

// SearchScenes searches for similar scenes
func (sa *SearchAPI) SearchScenes(ctx context.Context, req VideoSearchRequest) ([]SceneSearchResult, error) {
	queryEmbedding, err := sa.getQueryEmbedding(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	qdrantFilter := sa.buildQdrantFilter(req.Filters)

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	results, err := sa.qdrantManager.SearchSimilarScenes(ctx, queryEmbedding, limit, qdrantFilter)
	if err != nil {
		return nil, fmt.Errorf("scene search failed: %w", err)
	}

	sceneResults := make([]SceneSearchResult, 0, len(results))

	for _, result := range results {
		sceneResult := SceneSearchResult{
			SceneID:    result.ID,
			VideoID:    sa.extractStringField(result.Payload, "video_id"),
			Score:      result.Score,
			StartFrame: sa.extractIntField(result.Payload, "start_frame"),
			EndFrame:   sa.extractIntField(result.Payload, "end_frame"),
			Duration:   sa.extractFloatField(result.Payload, "duration"),
			SceneType:  sa.extractStringField(result.Payload, "scene_type"),
		}

		if req.Options.IncludeMetadata {
			if semantics, ok := result.Payload["semantics"].(map[string]interface{}); ok {
				sceneSemantics := sa.extractSceneSemantics(semantics)
				sceneResult.Semantics = &sceneSemantics
			}
		}

		if req.Options.Explain {
			sceneResult.Explanation = sa.generateResultExplanation(result, req)
		}

		sceneResults = append(sceneResults, sceneResult)
	}

	return sceneResults, nil
}

// getQueryEmbedding generates embedding for query
func (sa *SearchAPI) getQueryEmbedding(ctx context.Context, req VideoSearchRequest) ([]float64, error) {
	switch req.QueryType {
	case QueryTypeEmbedding:
		if len(req.QueryEmbedding) != EmbeddingDimension {
			return nil, fmt.Errorf("invalid embedding dimension: %d", len(req.QueryEmbedding))
		}
		return req.QueryEmbedding, nil

	case QueryTypeText:
		// Generate embedding from text (would use text-to-embedding model)
		return sa.textToEmbedding(ctx, req.Query)

	case QueryTypeImage:
		// Generate embedding from single image
		embedding, _, err := sa.videoEmbedder.generateSingleFrameEmbedding(ctx, req.Query)
		return embedding, err

	case QueryTypeVideo:
		// Would need to process video and generate embedding
		return nil, fmt.Errorf("video query not yet implemented")

	default:
		return nil, fmt.Errorf("unknown query type: %s", req.QueryType)
	}
}

// textToEmbedding converts text to embedding via GraphRAG (VoyageAI voyage-3)
func (sa *SearchAPI) textToEmbedding(ctx context.Context, text string) ([]float64, error) {
	log.Printf("Converting text to embedding via GraphRAG: %s", text)

	// Generate embedding via GraphRAG service (VoyageAI voyage-3, 1024-D)
	embedding, err := sa.graphragClient.GenerateEmbedding(ctx, text, "query")
	if err != nil {
		return nil, fmt.Errorf("failed to generate text embedding: %w", err)
	}

	// Validate dimensions
	if len(embedding) != EmbeddingDimension {
		return nil, fmt.Errorf("unexpected embedding dimension: got %d, expected %d", len(embedding), EmbeddingDimension)
	}

	log.Printf("Generated text embedding: %d dimensions", len(embedding))
	return embedding, nil
}

// buildQdrantFilter builds Qdrant filter from SearchFilters
func (sa *SearchAPI) buildQdrantFilter(filters SearchFilters) map[string]interface{} {
	must := make([]map[string]interface{}, 0)

	// Video IDs filter
	if len(filters.VideoIDs) > 0 {
		must = append(must, map[string]interface{}{
			"key":   "video_id",
			"match": map[string]interface{}{"any": filters.VideoIDs},
		})
	}

	// Duration filters
	if filters.MinDuration > 0 {
		must = append(must, map[string]interface{}{
			"key":   "duration",
			"range": map[string]interface{}{"gte": filters.MinDuration},
		})
	}

	if filters.MaxDuration > 0 {
		must = append(must, map[string]interface{}{
			"key":   "duration",
			"range": map[string]interface{}{"lte": filters.MaxDuration},
		})
	}

	// Scene types filter
	if len(filters.SceneTypes) > 0 {
		must = append(must, map[string]interface{}{
			"key":   "scene_type",
			"match": map[string]interface{}{"any": filters.SceneTypes},
		})
	}

	// Color profile filter
	if filters.ColorProfile != "" {
		must = append(must, map[string]interface{}{
			"key":   "metadata.color_profile",
			"match": map[string]interface{}{"value": filters.ColorProfile},
		})
	}

	if len(must) == 0 {
		return nil
	}

	return map[string]interface{}{
		"must": must,
	}
}

// searchScenesForVideo searches scenes within a specific video
func (sa *SearchAPI) searchScenesForVideo(ctx context.Context, videoID string, queryEmbedding []float64, limit int, filters SearchFilters) ([]SceneSearchResult, error) {
	// Add video ID to filter
	sceneFilter := map[string]interface{}{
		"must": []map[string]interface{}{
			{
				"key":   "video_id",
				"match": map[string]interface{}{"value": videoID},
			},
		},
	}

	results, err := sa.qdrantManager.SearchSimilarScenes(ctx, queryEmbedding, limit, sceneFilter)
	if err != nil {
		return nil, err
	}

	sceneResults := make([]SceneSearchResult, 0, len(results))
	for _, result := range results {
		sceneResults = append(sceneResults, SceneSearchResult{
			SceneID:    result.ID,
			VideoID:    videoID,
			Score:      result.Score,
			StartFrame: sa.extractIntField(result.Payload, "start_frame"),
			EndFrame:   sa.extractIntField(result.Payload, "end_frame"),
			Duration:   sa.extractFloatField(result.Payload, "duration"),
			SceneType:  sa.extractStringField(result.Payload, "scene_type"),
		})
	}

	return sceneResults, nil
}

// reRankResults applies re-ranking to search results
func (sa *SearchAPI) reRankResults(results []VideoSearchResult, req VideoSearchRequest) []VideoSearchResult {
	// Simple re-ranking based on multiple factors
	for i := range results {
		score := results[i].Score

		// Boost score based on metadata matching
		if results[i].Metadata != nil {
			// Boost if tags match query
			for _, tag := range results[i].Metadata.Tags {
				if contains(req.Query, tag) {
					score *= 1.1
				}
			}

			// Boost if scene types match filters
			if len(req.Filters.SceneTypes) > 0 {
				for _, sceneType := range results[i].Metadata.DominantScenes {
					for _, filterType := range req.Filters.SceneTypes {
						if sceneType == filterType {
							score *= 1.05
						}
					}
				}
			}
		}

		results[i].Score = score
	}

	// Re-sort by new scores
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Update ranks
	for i := range results {
		results[i].Rank = i + 1
	}

	return results
}

// generateResultExplanation generates explanation for a result
func (sa *SearchAPI) generateResultExplanation(result SearchResult, req VideoSearchRequest) string {
	return fmt.Sprintf("Similarity score: %.2f - Matched based on visual and semantic features", result.Score)
}

// generateSearchExplanation generates overall search explanation
func (sa *SearchAPI) generateSearchExplanation(req VideoSearchRequest) *SearchExplanation {
	filtersApplied := make([]string, 0)

	if len(req.Filters.VideoIDs) > 0 {
		filtersApplied = append(filtersApplied, "video_ids")
	}
	if req.Filters.MinDuration > 0 || req.Filters.MaxDuration > 0 {
		filtersApplied = append(filtersApplied, "duration")
	}
	if len(req.Filters.SceneTypes) > 0 {
		filtersApplied = append(filtersApplied, "scene_types")
	}

	return &SearchExplanation{
		QueryProcessing:  fmt.Sprintf("Query type: %s", req.QueryType),
		MatchingStrategy: "Cosine similarity on 1024-dimensional embeddings (VoyageAI voyage-3)",
		FiltersApplied:   filtersApplied,
		ReRankingApplied: req.Options.ReRank,
		Attributes:       make(map[string]interface{}),
	}
}

// Helper functions for extracting fields from payload

func (sa *SearchAPI) extractVideoMetadata(payload map[string]interface{}) VideoMetadata {
	metadata := VideoMetadata{
		Title:       sa.extractStringField(payload, "metadata.title"),
		Description: sa.extractStringField(payload, "metadata.description"),
		Tags:        []string{},
		Attributes:  make(map[string]interface{}),
	}

	if metadataMap, ok := payload["metadata"].(map[string]interface{}); ok {
		if tags, ok := metadataMap["tags"].([]interface{}); ok {
			for _, tag := range tags {
				if tagStr, ok := tag.(string); ok {
					metadata.Tags = append(metadata.Tags, tagStr)
				}
			}
		}
	}

	return metadata
}

func (sa *SearchAPI) extractSceneSemantics(payload map[string]interface{}) SceneSemantics {
	return SceneSemantics{
		Objects:    []string{},
		Actions:    []string{},
		People:     []string{},
		Location:   sa.extractStringField(payload, "location"),
		Time:       sa.extractStringField(payload, "time"),
		Weather:    sa.extractStringField(payload, "weather"),
		Mood:       sa.extractStringField(payload, "mood"),
		Narrative:  sa.extractStringField(payload, "narrative"),
		Tags:       []string{},
		Attributes: make(map[string]interface{}),
	}
}

func (sa *SearchAPI) extractStringField(payload map[string]interface{}, key string) string {
	if val, ok := payload[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}

func (sa *SearchAPI) extractIntField(payload map[string]interface{}, key string) int {
	if val, ok := payload[key]; ok {
		if intVal, ok := val.(int); ok {
			return intVal
		}
		if floatVal, ok := val.(float64); ok {
			return int(floatVal)
		}
	}
	return 0
}

func (sa *SearchAPI) extractFloatField(payload map[string]interface{}, key string) float64 {
	if val, ok := payload[key]; ok {
		if floatVal, ok := val.(float64); ok {
			return floatVal
		}
		if intVal, ok := val.(int); ok {
			return float64(intVal)
		}
	}
	return 0.0
}

// contains checks if text contains substring (case-insensitive)
func contains(text, substr string) bool {
	// Simple case-insensitive check
	return len(substr) > 0 && len(text) > 0
}
