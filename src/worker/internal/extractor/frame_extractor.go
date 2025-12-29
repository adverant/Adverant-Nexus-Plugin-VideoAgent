package extractor

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sync"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
	"github.com/adverant/nexus/videoagent-worker/internal/utils"
)

// FrameExtractor handles parallel frame extraction and analysis
type FrameExtractor struct {
	ffmpeg       *utils.FFmpegHelper
	mageAgent    *clients.MageAgentClient
	concurrency  int
}

// NewFrameExtractor creates a new frame extractor
func NewFrameExtractor(ffmpeg *utils.FFmpegHelper, mageAgent *clients.MageAgentClient, concurrency int) *FrameExtractor {
	return &FrameExtractor{
		ffmpeg:      ffmpeg,
		mageAgent:   mageAgent,
		concurrency: concurrency,
	}
}

// ExtractAndAnalyze extracts frames and analyzes them with AI (zero hardcoded models)
func (fe *FrameExtractor) ExtractAndAnalyze(
	ctx context.Context,
	videoPath string,
	jobID string,
	options models.ProcessingOptions,
	duration float64,
) ([]models.FrameAnalysis, error) {

	// Step 1: Extract frames using FFmpeg
	outputDir := filepath.Join(filepath.Dir(videoPath), fmt.Sprintf("%s_frames", jobID))

	// Get values with defaults
	samplingMode := "uniform" // default
	if options.FrameSamplingMode != nil {
		samplingMode = *options.FrameSamplingMode
	}

	sampleRate := 1 // default
	if options.FrameSampleRate != nil {
		sampleRate = *options.FrameSampleRate
	}

	maxFrames := options.GetMaxFrames() // uses helper method

	framePaths, err := fe.ffmpeg.ExtractFrames(
		videoPath,
		samplingMode,
		sampleRate,
		maxFrames,
		duration,
		outputDir,
	)
	if err != nil {
		return nil, fmt.Errorf("frame extraction failed: %w", err)
	}

	if len(framePaths) == 0 {
		return []models.FrameAnalysis{}, nil
	}

	// Step 2: Calculate complexity for model selection
	complexity := fe.calculateComplexity(options, len(framePaths), duration)

	// Step 3: Select AI model dynamically (ZERO HARDCODED MODEL)
	qualityPref := "balanced" // default
	if options.QualityPreference != nil {
		qualityPref = *options.QualityPreference
	}

	detectObjects := options.DetectObjects != nil && *options.DetectObjects
	extractText := options.ExtractText != nil && *options.ExtractText

	modelReq := models.MageAgentModelRequest{
		TaskType:   "vision",
		Complexity: complexity,
		Context: map[string]interface{}{
			"task":           "frame_analysis",
			"frame_count":    len(framePaths),
			"quality":        qualityPref,
			"detect_objects": detectObjects,
			"extract_text":   extractText,
		},
	}

	modelResp, err := fe.mageAgent.SelectModel(ctx, modelReq)
	if err != nil {
		return nil, fmt.Errorf("model selection failed: %w", err)
	}

	// Step 4: Analyze frames in parallel
	frameAnalyses, err := fe.analyzeFramesParallel(ctx, framePaths, modelResp, options, duration)
	if err != nil {
		return nil, fmt.Errorf("frame analysis failed: %w", err)
	}

	return frameAnalyses, nil
}

// analyzeFramesParallel analyzes frames using goroutines for parallel processing
func (fe *FrameExtractor) analyzeFramesParallel(
	ctx context.Context,
	framePaths []string,
	modelResp *models.MageAgentModelResponse,
	options models.ProcessingOptions,
	videoDuration float64,
) ([]models.FrameAnalysis, error) {

	frameCount := len(framePaths)
	results := make([]models.FrameAnalysis, frameCount)
	errors := make([]error, frameCount)

	// Create worker pool
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, fe.concurrency)

	for i, framePath := range framePaths {
		wg.Add(1)

		go func(index int, path string) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Analyze single frame
			analysis, err := fe.analyzeSingleFrame(ctx, path, index, videoDuration, frameCount, modelResp, options)
			if err != nil {
				errors[index] = err
				return
			}

			results[index] = *analysis
		}(i, framePath)
	}

	// Wait for all goroutines
	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("frame %d analysis failed: %w", i, err)
		}
	}

	return results, nil
}

// analyzeSingleFrame analyzes a single frame with AI
func (fe *FrameExtractor) analyzeSingleFrame(
	ctx context.Context,
	framePath string,
	frameNumber int,
	videoDuration float64,
	totalFrames int,
	modelResp *models.MageAgentModelResponse,
	options models.ProcessingOptions,
) (*models.FrameAnalysis, error) {

	// Encode frame to base64
	frameBase64, err := fe.ffmpeg.EncodeFrameToBase64(framePath)
	if err != nil {
		return nil, fmt.Errorf("failed to encode frame: %w", err)
	}

	// Calculate timestamp
	timestamp := (videoDuration / float64(totalFrames)) * float64(frameNumber)

	// Build analysis prompt based on options
	prompt := fe.buildAnalysisPrompt(options)

	// Analyze frame with MageAgent (using dynamically selected model)
	visionReq := models.MageAgentVisionRequest{
		Image:     frameBase64,
		Prompt:    prompt,
		ModelID:   modelResp.ModelID,
		MaxTokens: 500,
		AdditionalContext: map[string]interface{}{
			"frame_number": frameNumber,
			"timestamp":    timestamp,
		},
	}

	visionResp, err := fe.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("frame analysis failed: %w", err)
	}

	// Generate embedding for semantic search (if requested)
	var embedding []float32
	shouldGenerateSummary := options.GenerateSummary != nil && *options.GenerateSummary
	if shouldGenerateSummary {
		embedding, err = fe.mageAgent.GenerateEmbedding(ctx, visionResp.Description, modelResp.ModelID)
		if err != nil {
			// Non-fatal - continue without embedding
			embedding = []float32{}
		}
	}

	// Create frame analysis result
	frameAnalysis := &models.FrameAnalysis{
		FrameID:     models.NewFrameID(),
		Timestamp:   timestamp,
		FrameNumber: frameNumber,
		FilePath:    framePath,
		Objects:     visionResp.Objects,
		Text:        visionResp.Text,
		Description: visionResp.Description,
		Embedding:   embedding,
		ModelUsed:   modelResp.ModelID,
		Confidence:  visionResp.Confidence,
		Metadata:    visionResp.Metadata,
	}

	return frameAnalysis, nil
}

// buildAnalysisPrompt builds the analysis prompt based on processing options
func (fe *FrameExtractor) buildAnalysisPrompt(options models.ProcessingOptions) string {
	prompt := "Analyze this video frame."

	if options.DetectObjects != nil && *options.DetectObjects {
		prompt += " Identify and locate all objects with bounding boxes."
	}

	if options.ExtractText != nil && *options.ExtractText {
		prompt += " Extract all visible text using OCR."
	}

	if options.ClassifyContent != nil && *options.ClassifyContent {
		prompt += " Classify the content and identify the scene type."
	}

	if options.CustomAnalysis != nil && *options.CustomAnalysis != "" {
		prompt += " " + *options.CustomAnalysis
	}

	prompt += " Provide a detailed description of the frame content."

	return prompt
}

// calculateComplexity calculates task complexity for model selection
func (fe *FrameExtractor) calculateComplexity(options models.ProcessingOptions, frameCount int, duration float64) float64 {
	complexity := 0.3 // Base complexity

	// Increase complexity based on options
	if options.DetectObjects != nil && *options.DetectObjects {
		complexity += 0.2
	}

	if options.ExtractText != nil && *options.ExtractText {
		complexity += 0.15
	}

	if options.ClassifyContent != nil && *options.ClassifyContent {
		complexity += 0.1
	}

	if options.ShouldDetectScenes() {
		complexity += 0.15
	}

	// Adjust for quality preference
	qualityPref := "balanced" // default
	if options.QualityPreference != nil {
		qualityPref = *options.QualityPreference
	}

	switch qualityPref {
	case "speed":
		complexity -= 0.1
	case "accuracy":
		complexity += 0.2
	}

	// Adjust for frame count (more frames = lower complexity per frame)
	if frameCount > 50 {
		complexity -= 0.1
	}

	// Clamp between 0 and 1
	if complexity < 0 {
		complexity = 0
	}
	if complexity > 1 {
		complexity = 1
	}

	return complexity
}

// DetectScenes detects scene changes in video
func (fe *FrameExtractor) DetectScenes(
	ctx context.Context,
	frames []models.FrameAnalysis,
	modelID string,
) ([]models.SceneDetection, error) {

	if len(frames) < 2 {
		return []models.SceneDetection{}, nil
	}

	scenes := []models.SceneDetection{}
	currentScene := models.SceneDetection{
		SceneID:    models.NewSceneID(),
		StartFrame: 0,
		StartTime:  frames[0].Timestamp,
		KeyFrameID: frames[0].FrameID,
	}

	// Simple scene detection based on frame description similarity
	// In production, use more sophisticated algorithms or ML models
	for i := 1; i < len(frames); i++ {
		prevFrame := frames[i-1]
		currFrame := frames[i]

		// Detect scene change (simplified - in production use embeddings similarity)
		isSceneChange := fe.detectSceneChange(prevFrame, currFrame)

		if isSceneChange {
			// End current scene
			currentScene.EndFrame = i - 1
			currentScene.EndTime = prevFrame.Timestamp

			// Generate scene description using AI
			sceneDesc, err := fe.generateSceneDescription(ctx, frames[currentScene.StartFrame:i], modelID)
			if err == nil {
				currentScene.Description = sceneDesc
			}

			scenes = append(scenes, currentScene)

			// Start new scene
			currentScene = models.SceneDetection{
				SceneID:    models.NewSceneID(),
				StartFrame: i,
				StartTime:  currFrame.Timestamp,
				KeyFrameID: currFrame.FrameID,
			}
		}
	}

	// Add final scene
	lastFrame := frames[len(frames)-1]
	currentScene.EndFrame = len(frames) - 1
	currentScene.EndTime = lastFrame.Timestamp
	sceneDesc, err := fe.generateSceneDescription(ctx, frames[currentScene.StartFrame:], modelID)
	if err == nil {
		currentScene.Description = sceneDesc
	}
	scenes = append(scenes, currentScene)

	return scenes, nil
}

// detectSceneChange detects if there's a scene change between two frames using cosine similarity
func (fe *FrameExtractor) detectSceneChange(prev, curr models.FrameAnalysis) bool {
	// Use cosine similarity on embeddings for accurate scene detection
	if len(prev.Embedding) > 0 && len(curr.Embedding) > 0 {
		similarity := cosineSimilarity(prev.Embedding, curr.Embedding)

		// Threshold: similarity < 0.85 indicates scene change
		// Lower similarity = more different frames = likely scene change
		if similarity < 0.85 {
			return true
		}
	} else {
		// Fallback: use heuristic if embeddings not available
		prevObjCount := len(prev.Objects)
		currObjCount := len(curr.Objects)

		// Large change in object count suggests scene change
		if abs(prevObjCount-currObjCount) > 5 {
			return true
		}

		// Low confidence in current frame might indicate transition
		if curr.Confidence < 0.5 {
			return true
		}
	}

	return false
}

// cosineSimilarity calculates cosine similarity between two embedding vectors
// Returns value between -1 and 1 (1 = identical, 0 = orthogonal, -1 = opposite)
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct float64
	var magnitudeA float64
	var magnitudeB float64

	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		magnitudeA += float64(a[i]) * float64(a[i])
		magnitudeB += float64(b[i]) * float64(b[i])
	}

	// Avoid division by zero
	if magnitudeA == 0 || magnitudeB == 0 {
		return 0.0
	}

	// Calculate cosine similarity
	similarity := dotProduct / (math.Sqrt(magnitudeA) * math.Sqrt(magnitudeB))

	return similarity
}

// generateSceneDescription generates a description for a scene using AI
func (fe *FrameExtractor) generateSceneDescription(
	ctx context.Context,
	sceneFrames []models.FrameAnalysis,
	modelID string,
) (string, error) {

	// Collect frame descriptions
	descriptions := make([]string, len(sceneFrames))
	for i, frame := range sceneFrames {
		descriptions[i] = frame.Description
	}

	// Synthesize scene description from frame descriptions
	sceneDesc, err := fe.mageAgent.Synthesize(ctx, descriptions, "summary", "scene description")
	if err != nil {
		return "", err
	}

	return sceneDesc, nil
}

// abs returns absolute value
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
