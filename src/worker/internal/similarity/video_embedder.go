package similarity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// EmbeddingDimension is the dimension of video embeddings (VoyageAI voyage-3)
const EmbeddingDimension = 1024

// VideoEmbedding represents a video-level embedding
type VideoEmbedding struct {
	VideoID         string                 `json:"videoId"`
	Embedding       []float64              `json:"embedding"`        // 1024-dimensional vector (VoyageAI voyage-3)
	FrameCount      int                    `json:"frameCount"`
	Duration        float64                `json:"duration"`         // Seconds
	GeneratedAt     time.Time              `json:"generatedAt"`
	Model           string                 `json:"model"`            // Model used for embedding
	FrameEmbeddings []FrameEmbedding       `json:"frameEmbeddings"`  // Individual frame embeddings
	Metadata        VideoMetadata          `json:"metadata"`
	Hash            string                 `json:"hash"`             // Content hash
}

// FrameEmbedding represents a single frame embedding
type FrameEmbedding struct {
	FrameNum    int       `json:"frameNum"`
	Timestamp   float64   `json:"timestamp"`    // Seconds
	Embedding   []float64 `json:"embedding"`    // 1024-dimensional vector (VoyageAI voyage-3)
	Confidence  float64   `json:"confidence"`   // 0-1
	Features    FrameFeatures `json:"features"` // Visual features
}

// FrameFeatures represents extracted visual features
type FrameFeatures struct {
	ColorHistogram   []float64              `json:"colorHistogram"`   // RGB histogram (3x64 bins)
	DominantColors   []string               `json:"dominantColors"`   // Top 5 colors
	Brightness       float64                `json:"brightness"`       // 0-1
	Contrast         float64                `json:"contrast"`         // 0-1
	Sharpness        float64                `json:"sharpness"`        // 0-1
	Motion           float64                `json:"motion"`           // 0-1, amount of motion
	Objects          []string               `json:"objects"`          // Detected objects
	Scene            string                 `json:"scene"`            // Scene type
	Attributes       map[string]interface{} `json:"attributes"`
}

// VideoMetadata contains video-level metadata
type VideoMetadata struct {
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	Tags            []string               `json:"tags"`
	DominantScenes  []string               `json:"dominantScenes"`   // Most common scene types
	DominantObjects []string               `json:"dominantObjects"`  // Most common objects
	AvgBrightness   float64                `json:"avgBrightness"`
	AvgContrast     float64                `json:"avgContrast"`
	AvgMotion       float64                `json:"avgMotion"`
	ColorProfile    string                 `json:"colorProfile"`     // warm, cool, neutral, vibrant
	Attributes      map[string]interface{} `json:"attributes"`
}

// VideoEmbedder generates embeddings for videos
type VideoEmbedder struct {
	mageAgent     *clients.MageAgentClient
	graphragClient *clients.GraphRAGClient
	model         string
	batchSize     int     // Frames to process in batch
	samplingRate  int     // Sample every N frames
	aggregation   string  // How to aggregate frame embeddings (mean, max, attention)
}

// NewVideoEmbedder creates a new video embedder
func NewVideoEmbedder(mageAgent *clients.MageAgentClient, graphragClient *clients.GraphRAGClient) *VideoEmbedder {
	return &VideoEmbedder{
		mageAgent:      mageAgent,
		graphragClient: graphragClient,
		model:          "voyage-3",
		batchSize:      16,
		samplingRate:   30, // Sample every 30 frames (1 sec at 30fps)
		aggregation:    "mean",
	}
}

// GenerateEmbedding generates embedding for an entire video
func (ve *VideoEmbedder) GenerateEmbedding(ctx context.Context, videoID string, frames []string, metadata VideoMetadata) (*VideoEmbedding, error) {
	startTime := time.Now()

	// Sample frames
	sampledFrames, sampledIndices := ve.sampleFrames(frames)
	log.Printf("Sampled %d frames from %d total frames (rate: 1/%d)",
		len(sampledFrames), len(frames), ve.samplingRate)

	// Generate frame embeddings
	frameEmbeddings, err := ve.generateFrameEmbeddings(ctx, sampledFrames, sampledIndices)
	if err != nil {
		return nil, fmt.Errorf("frame embedding generation failed: %w", err)
	}

	// Aggregate frame embeddings into video embedding
	videoEmbedding := ve.aggregateEmbeddings(frameEmbeddings)

	// Compute video metadata
	computedMetadata := ve.computeVideoMetadata(frameEmbeddings, metadata)

	// Compute content hash
	hash := ve.computeHash(videoEmbedding)

	embedding := &VideoEmbedding{
		VideoID:         videoID,
		Embedding:       videoEmbedding,
		FrameCount:      len(frames),
		Duration:        float64(len(frames)) / 30.0, // Assume 30fps
		GeneratedAt:     time.Now(),
		Model:           ve.model,
		FrameEmbeddings: frameEmbeddings,
		Metadata:        computedMetadata,
		Hash:            hash,
	}

	elapsed := time.Since(startTime).Seconds()
	log.Printf("Generated video embedding for %s: %d frames, %.2fs elapsed",
		videoID, len(frames), elapsed)

	return embedding, nil
}

// sampleFrames samples frames at specified rate
func (ve *VideoEmbedder) sampleFrames(frames []string) ([]string, []int) {
	sampled := make([]string, 0)
	indices := make([]int, 0)

	for i := 0; i < len(frames); i += ve.samplingRate {
		sampled = append(sampled, frames[i])
		indices = append(indices, i)
	}

	// Always include last frame
	if len(frames) > 0 && (len(frames)-1)%ve.samplingRate != 0 {
		sampled = append(sampled, frames[len(frames)-1])
		indices = append(indices, len(frames)-1)
	}

	return sampled, indices
}

// generateFrameEmbeddings generates embeddings for individual frames
func (ve *VideoEmbedder) generateFrameEmbeddings(ctx context.Context, frames []string, indices []int) ([]FrameEmbedding, error) {
	embeddings := make([]FrameEmbedding, 0, len(frames))

	// Process in batches
	for i := 0; i < len(frames); i += ve.batchSize {
		end := i + ve.batchSize
		if end > len(frames) {
			end = len(frames)
		}

		batch := frames[i:end]
		batchIndices := indices[i:end]

		batchEmbeddings, err := ve.generateFrameBatch(ctx, batch, batchIndices)
		if err != nil {
			return nil, fmt.Errorf("batch %d failed: %w", i/ve.batchSize, err)
		}

		embeddings = append(embeddings, batchEmbeddings...)

		log.Printf("Processed batch %d/%d (%d frames)",
			i/ve.batchSize+1, (len(frames)+ve.batchSize-1)/ve.batchSize, len(batch))
	}

	return embeddings, nil
}

// generateFrameBatch generates embeddings for a batch of frames
func (ve *VideoEmbedder) generateFrameBatch(ctx context.Context, frames []string, indices []int) ([]FrameEmbedding, error) {
	embeddings := make([]FrameEmbedding, len(frames))

	for i, frame := range frames {
		embedding, features, err := ve.generateSingleFrameEmbedding(ctx, frame)
		if err != nil {
			log.Printf("Warning: Frame %d embedding failed: %v", indices[i], err)
			// Use zero embedding as fallback
			embedding = make([]float64, EmbeddingDimension)
			features = FrameFeatures{} // Default zero values
		}

		embeddings[i] = FrameEmbedding{
			FrameNum:   indices[i],
			Timestamp:  float64(indices[i]) / 30.0, // Assume 30fps
			Embedding:  embedding,
			Confidence: 0.9,
			Features:   features,
		}
	}

	return embeddings, nil
}

// generateSingleFrameEmbedding generates embedding for a single frame
//
// Two-step process:
// 1. Use MageAgent vision analysis to get frame description and features
// 2. Generate VoyageAI embedding from description via GraphRAG
func (ve *VideoEmbedder) generateSingleFrameEmbedding(ctx context.Context, frameData string) ([]float64, FrameFeatures, error) {
	// Step 1: Get visual analysis from MageAgent
	prompt := `Analyze this video frame and describe its visual content in detail, including:
1. Visual features (color, brightness, contrast, sharpness)
2. Detected objects (list up to 10)
3. Scene type (indoor/outdoor/action/dialogue/etc)
4. Motion estimation (low, medium, high)
5. Dominant colors (top 5)

Respond with JSON:
{
  "description": "Detailed text description of the frame for semantic search",
  "features": {
    "colorHistogram": [192 float values for RGB histogram],
    "dominantColors": ["color1", "color2", "color3", "color4", "color5"],
    "brightness": 0.0-1.0,
    "contrast": 0.0-1.0,
    "sharpness": 0.0-1.0,
    "motion": 0.0-1.0,
    "objects": ["object1", "object2", ...],
    "scene": "indoor|outdoor|action|dialogue|...",
    "attributes": {}
  }
}`

	visionReq := models.MageAgentVisionRequest{
		Image:     frameData,
		Prompt:    prompt,
		MaxTokens: 1500,
	}

	visionResp, err := ve.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, FrameFeatures{}, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Parse vision response
	description, features, err := ve.parseFrameAnalysis(visionResp.Description)
	if err != nil {
		log.Printf("Failed to parse frame analysis: %v", err)
		// Use raw response as description
		description = visionResp.Description
		features = ve.generateDefaultFeatures()
	}

	// Step 2: Generate embedding from description via GraphRAG (VoyageAI voyage-3)
	embedding, err := ve.graphragClient.GenerateEmbedding(ctx, description, "document")
	if err != nil {
		log.Printf("Failed to generate embedding via GraphRAG: %v", err)
		// Return default embedding as fallback
		return ve.generateDefaultEmbedding(), features, nil
	}

	// Validate embedding dimensions
	if len(embedding) != EmbeddingDimension {
		log.Printf("Warning: Unexpected embedding dimension: got %d, expected %d", len(embedding), EmbeddingDimension)
		return ve.generateDefaultEmbedding(), features, nil
	}

	return embedding, features, nil
}

// parseFrameAnalysis parses AI vision response into description and features
func (ve *VideoEmbedder) parseFrameAnalysis(response string) (string, FrameFeatures, error) {
	jsonStr := extractJSON(response)

	var parsed struct {
		Description string        `json:"description"`
		Features    FrameFeatures `json:"features"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return "", FrameFeatures{}, fmt.Errorf("failed to unmarshal: %w", err)
	}

	// Validate description exists
	if parsed.Description == "" {
		return "", FrameFeatures{}, fmt.Errorf("empty description in response")
	}

	return parsed.Description, parsed.Features, nil
}

// aggregateEmbeddings aggregates frame embeddings into video embedding
func (ve *VideoEmbedder) aggregateEmbeddings(frameEmbeddings []FrameEmbedding) []float64 {
	if len(frameEmbeddings) == 0 {
		return make([]float64, EmbeddingDimension)
	}

	switch ve.aggregation {
	case "mean":
		return ve.meanAggregation(frameEmbeddings)
	case "max":
		return ve.maxAggregation(frameEmbeddings)
	case "attention":
		return ve.attentionAggregation(frameEmbeddings)
	default:
		return ve.meanAggregation(frameEmbeddings)
	}
}

// meanAggregation computes mean of frame embeddings
func (ve *VideoEmbedder) meanAggregation(frameEmbeddings []FrameEmbedding) []float64 {
	aggregated := make([]float64, EmbeddingDimension)

	for _, frame := range frameEmbeddings {
		for i, val := range frame.Embedding {
			aggregated[i] += val
		}
	}

	// Normalize
	n := float64(len(frameEmbeddings))
	for i := range aggregated {
		aggregated[i] /= n
	}

	return aggregated
}

// maxAggregation computes max pooling of frame embeddings
func (ve *VideoEmbedder) maxAggregation(frameEmbeddings []FrameEmbedding) []float64 {
	aggregated := make([]float64, EmbeddingDimension)

	// Initialize with first frame
	if len(frameEmbeddings) > 0 {
		copy(aggregated, frameEmbeddings[0].Embedding)
	}

	// Max pooling
	for _, frame := range frameEmbeddings[1:] {
		for i, val := range frame.Embedding {
			if val > aggregated[i] {
				aggregated[i] = val
			}
		}
	}

	return aggregated
}

// attentionAggregation computes attention-weighted aggregation
func (ve *VideoEmbedder) attentionAggregation(frameEmbeddings []FrameEmbedding) []float64 {
	if len(frameEmbeddings) == 0 {
		return make([]float64, EmbeddingDimension)
	}

	// Compute attention weights based on confidence
	weights := make([]float64, len(frameEmbeddings))
	sumWeights := 0.0

	for i, frame := range frameEmbeddings {
		weights[i] = frame.Confidence
		sumWeights += weights[i]
	}

	// Normalize weights
	if sumWeights > 0 {
		for i := range weights {
			weights[i] /= sumWeights
		}
	} else {
		// Uniform weights
		w := 1.0 / float64(len(frameEmbeddings))
		for i := range weights {
			weights[i] = w
		}
	}

	// Weighted aggregation
	aggregated := make([]float64, EmbeddingDimension)
	for i, frame := range frameEmbeddings {
		weight := weights[i]
		for j, val := range frame.Embedding {
			aggregated[j] += val * weight
		}
	}

	return aggregated
}

// computeVideoMetadata computes video-level metadata from frames
func (ve *VideoEmbedder) computeVideoMetadata(frameEmbeddings []FrameEmbedding, inputMetadata VideoMetadata) VideoMetadata {
	if len(frameEmbeddings) == 0 {
		return inputMetadata
	}

	// Aggregate statistics
	avgBrightness := 0.0
	avgContrast := 0.0
	avgMotion := 0.0

	sceneMap := make(map[string]int)
	objectMap := make(map[string]int)
	colorMap := make(map[string]int)

	for _, frame := range frameEmbeddings {
		avgBrightness += frame.Features.Brightness
		avgContrast += frame.Features.Contrast
		avgMotion += frame.Features.Motion

		sceneMap[frame.Features.Scene]++

		for _, obj := range frame.Features.Objects {
			objectMap[obj]++
		}

		for _, color := range frame.Features.DominantColors {
			colorMap[color]++
		}
	}

	n := float64(len(frameEmbeddings))
	avgBrightness /= n
	avgContrast /= n
	avgMotion /= n

	// Find dominant scenes and objects
	dominantScenes := ve.topN(sceneMap, 5)
	dominantObjects := ve.topN(objectMap, 10)
	dominantColors := ve.topN(colorMap, 5)

	// Determine color profile
	colorProfile := ve.determineColorProfile(dominantColors)

	return VideoMetadata{
		Title:           inputMetadata.Title,
		Description:     inputMetadata.Description,
		Tags:            inputMetadata.Tags,
		DominantScenes:  dominantScenes,
		DominantObjects: dominantObjects,
		AvgBrightness:   avgBrightness,
		AvgContrast:     avgContrast,
		AvgMotion:       avgMotion,
		ColorProfile:    colorProfile,
		Attributes:      inputMetadata.Attributes,
	}
}

// topN returns top N items from a frequency map
func (ve *VideoEmbedder) topN(freqMap map[string]int, n int) []string {
	type pair struct {
		key   string
		value int
	}

	pairs := make([]pair, 0, len(freqMap))
	for k, v := range freqMap {
		pairs = append(pairs, pair{k, v})
	}

	// Simple selection sort for top N
	result := make([]string, 0, n)
	for i := 0; i < n && i < len(pairs); i++ {
		maxIdx := i
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].value > pairs[maxIdx].value {
				maxIdx = j
			}
		}
		pairs[i], pairs[maxIdx] = pairs[maxIdx], pairs[i]
		result = append(result, pairs[i].key)
	}

	return result
}

// determineColorProfile determines overall color profile
func (ve *VideoEmbedder) determineColorProfile(dominantColors []string) string {
	if len(dominantColors) == 0 {
		return "neutral"
	}

	// Simple heuristic based on color names
	warmCount := 0
	coolCount := 0

	warmColors := map[string]bool{"red": true, "orange": true, "yellow": true, "brown": true}
	coolColors := map[string]bool{"blue": true, "green": true, "purple": true, "cyan": true}

	for _, color := range dominantColors {
		if warmColors[color] {
			warmCount++
		} else if coolColors[color] {
			coolCount++
		}
	}

	if warmCount > coolCount*2 {
		return "warm"
	} else if coolCount > warmCount*2 {
		return "cool"
	} else if warmCount+coolCount > len(dominantColors)/2 {
		return "vibrant"
	}

	return "neutral"
}

// computeHash computes content hash for embedding
func (ve *VideoEmbedder) computeHash(embedding []float64) string {
	// Convert embedding to bytes
	data := make([]byte, 0, len(embedding)*8)
	for _, val := range embedding {
		bits := math.Float64bits(val)
		for i := 0; i < 8; i++ {
			data = append(data, byte(bits>>(i*8)))
		}
	}

	// Compute SHA256 hash
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// generateDefaultEmbedding generates a default embedding
func (ve *VideoEmbedder) generateDefaultEmbedding() []float64 {
	embedding := make([]float64, EmbeddingDimension)
	// Initialize with small random values
	for i := range embedding {
		embedding[i] = (float64(i%10) - 5.0) / 10.0
	}
	return embedding
}

// generateDefaultFeatures generates default frame features
func (ve *VideoEmbedder) generateDefaultFeatures() FrameFeatures {
	return FrameFeatures{
		ColorHistogram: make([]float64, 192),
		DominantColors: []string{"gray"},
		Brightness:     0.5,
		Contrast:       0.5,
		Sharpness:      0.5,
		Motion:         0.0,
		Objects:        []string{},
		Scene:          "unknown",
		Attributes:     make(map[string]interface{}),
	}
}

// extractJSON extracts JSON from markdown code blocks
func extractJSON(response string) string {
	// Simple extraction - look for { and }
	start := -1
	end := -1

	for i, ch := range response {
		if ch == '{' && start == -1 {
			start = i
		}
		if ch == '}' {
			end = i + 1
		}
	}

	if start >= 0 && end > start {
		return response[start:end]
	}

	return response
}
