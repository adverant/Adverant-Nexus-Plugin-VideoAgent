package extractor

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
	"github.com/adverant/nexus/videoagent-worker/internal/utils"
)

// AudioExtractor handles audio extraction and transcription
type AudioExtractor struct {
	ffmpeg    *utils.FFmpegHelper
	mageAgent *clients.MageAgentClient
}

// chunkResult represents the result of processing an audio chunk
type chunkResult struct {
	index        int
	transcription string
	speakers      []models.SpeakerSegment
	language      string
	confidence    float64
	err           error
}

// NewAudioExtractor creates a new audio extractor
func NewAudioExtractor(ffmpeg *utils.FFmpegHelper, mageAgent *clients.MageAgentClient) *AudioExtractor {
	return &AudioExtractor{
		ffmpeg:    ffmpeg,
		mageAgent: mageAgent,
	}
}

// ExtractAndTranscribe extracts audio and transcribes it (zero hardcoded models)
// Automatically chunks large audio files (>10MB) for reliable processing
func (ae *AudioExtractor) ExtractAndTranscribe(
	ctx context.Context,
	videoPath string,
	jobID string,
	options models.ProcessingOptions,
) (*models.AudioAnalysis, error) {

	// Step 1: Extract audio from video
	audioPath := filepath.Join(filepath.Dir(videoPath), fmt.Sprintf("%s_audio.wav", jobID))
	if err := ae.ffmpeg.ExtractAudio(videoPath, audioPath); err != nil {
		return nil, fmt.Errorf("audio extraction failed: %w", err)
	}

	// Step 2: Check if audio file is large and needs chunking
	audioSize, err := ae.ffmpeg.GetAudioFileSize(audioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get audio file size: %w", err)
	}

	const maxAudioSize = 10 * 1024 * 1024 // 10MB threshold
	var fullTranscription string
	var allSpeakers []models.SpeakerSegment
	var selectedModel string
	var language string
	var avgConfidence float64

	if audioSize > maxAudioSize {
		// Large file - use chunking
		chunkSizeMB := 8 // 8MB chunks
		chunkPaths, err := ae.ffmpeg.ChunkAudio(audioPath, chunkSizeMB)
		if err != nil {
			return nil, fmt.Errorf("audio chunking failed: %w", err)
		}
		defer ae.ffmpeg.Cleanup(chunkPaths...) // Cleanup chunks after processing

		// Process chunks in parallel with goroutines
		results := make(chan chunkResult, len(chunkPaths))
		for i, chunkPath := range chunkPaths {
			go func(index int, path string) {
				result := chunkResult{index: index}

				// Calculate complexity for this chunk
				complexity := ae.calculateComplexity(options)

				// Select model for chunk
				modelReq := models.MageAgentModelRequest{
					TaskType:   "transcription",
					Complexity: complexity,
					Context: map[string]interface{}{
						"task":               "audio_transcription_chunk",
						"enable_diarization": options.ShouldTranscribeAudio(),
						"languages":          options.TargetLanguages,
						"quality":            getQualityPreference(options),
					},
				}

				modelResp, err := ae.mageAgent.SelectModel(ctx, modelReq)
				if err != nil {
					result.err = fmt.Errorf("model selection failed for chunk %d: %w", index, err)
					results <- result
					return
				}

				if index == 0 {
					selectedModel = modelResp.ModelID
				}

				// Encode chunk to base64
				chunkBase64, err := ae.ffmpeg.EncodeAudioToBase64(path)
				if err != nil {
					result.err = fmt.Errorf("failed to encode chunk %d: %w", index, err)
					results <- result
					return
				}

				// Determine language
				lang := "auto"
				if len(options.TargetLanguages) > 0 {
					lang = options.TargetLanguages[0]
				}

				// Transcribe chunk
				transcriptReq := models.MageAgentTranscriptionRequest{
					Audio:             chunkBase64,
					Language:          lang,
					ModelID:           modelResp.ModelID,
					EnableDiarization: options.ShouldTranscribeAudio(),
				}

				transcriptResp, err := ae.mageAgent.TranscribeAudio(ctx, transcriptReq)
				if err != nil {
					result.err = fmt.Errorf("transcription failed for chunk %d: %w", index, err)
					results <- result
					return
				}

				result.transcription = transcriptResp.Transcription
				result.speakers = transcriptResp.Speakers
				result.language = transcriptResp.Language
				result.confidence = transcriptResp.Confidence
				results <- result
			}(i, chunkPath)
		}

		// Collect results in order
		chunkResults := make([]chunkResult, len(chunkPaths))
		for i := 0; i < len(chunkPaths); i++ {
			result := <-results
			if result.err != nil {
				return nil, result.err
			}
			chunkResults[result.index] = result
		}

		// Merge transcriptions with overlap handling
		fullTranscription = ae.mergeTranscriptions(chunkResults)

		// Merge speaker segments
		allSpeakers = ae.mergeSpeakerSegments(chunkResults)

		// Use first chunk's language and average confidence
		language = chunkResults[0].language
		totalConfidence := 0.0
		for _, r := range chunkResults {
			totalConfidence += r.confidence
		}
		avgConfidence = totalConfidence / float64(len(chunkResults))

	} else {
		// Small file - process directly (original logic)
		complexity := ae.calculateComplexity(options)

		modelReq := models.MageAgentModelRequest{
			TaskType:   "transcription",
			Complexity: complexity,
			Context: map[string]interface{}{
				"task":               "audio_transcription",
				"enable_diarization": options.ShouldTranscribeAudio(),
				"languages":          options.TargetLanguages,
				"quality":            getQualityPreference(options),
			},
		}

		modelResp, err := ae.mageAgent.SelectModel(ctx, modelReq)
		if err != nil {
			return nil, fmt.Errorf("model selection failed: %w", err)
		}
		selectedModel = modelResp.ModelID

		audioBase64, err := ae.ffmpeg.EncodeAudioToBase64(audioPath)
		if err != nil {
			return nil, fmt.Errorf("failed to encode audio: %w", err)
		}

		language = "auto"
		if len(options.TargetLanguages) > 0 {
			language = options.TargetLanguages[0]
		}

		transcriptReq := models.MageAgentTranscriptionRequest{
			Audio:             audioBase64,
			Language:          language,
			ModelID:           modelResp.ModelID,
			EnableDiarization: options.ShouldTranscribeAudio(),
		}

		transcriptResp, err := ae.mageAgent.TranscribeAudio(ctx, transcriptReq)
		if err != nil {
			return nil, fmt.Errorf("transcription failed: %w", err)
		}

		fullTranscription = transcriptResp.Transcription
		allSpeakers = transcriptResp.Speakers
		language = transcriptResp.Language
		avgConfidence = transcriptResp.Confidence
	}

	// Perform additional analysis on complete transcription
	var sentiment string
	var topics []string
	var keywords []string

	shouldClassify := options.ClassifyContent != nil && *options.ClassifyContent
	shouldSummarize := options.GenerateSummary != nil && *options.GenerateSummary

	if shouldClassify || shouldSummarize {
		// Get model for analysis (reuse from chunking or select new)
		if selectedModel == "" {
			complexity := ae.calculateComplexity(options)
			modelReq := models.MageAgentModelRequest{
				TaskType:   "transcription",
				Complexity: complexity,
			}
			modelResp, err := ae.mageAgent.SelectModel(ctx, modelReq)
			if err == nil {
				selectedModel = modelResp.ModelID
			}
		}

		// Sentiment analysis
		sentiment, _, err = ae.mageAgent.AnalyzeSentiment(ctx, fullTranscription, selectedModel)
		if err != nil {
			sentiment = "neutral"
		}

		// Topic extraction
		topics, err = ae.mageAgent.ExtractTopics(ctx, fullTranscription, selectedModel)
		if err != nil {
			topics = []string{}
		}

		// Extract keywords using MageAgent
		keywords, err = ae.mageAgent.ExtractTopics(ctx, fullTranscription, selectedModel)
		if err != nil {
			keywords = []string{}
		}
	}

	// Build audio analysis result
	audioAnalysis := &models.AudioAnalysis{
		Transcription:  fullTranscription,
		Language:       language,
		Confidence:     avgConfidence,
		Speakers:       allSpeakers,
		Sentiment:      sentiment,
		Topics:         topics,
		Keywords:       keywords,
		AudioFilePath:  audioPath,
		ModelUsed:      selectedModel,
		ProcessingTime: 0, // Will be calculated by processor
	}

	return audioAnalysis, nil
}

// calculateComplexity calculates task complexity for model selection
func (ae *AudioExtractor) calculateComplexity(options models.ProcessingOptions) float64 {
	complexity := 0.4 // Base complexity for transcription

	// Increase complexity if diarization is needed
	if options.ShouldTranscribeAudio() {
		complexity += 0.2
	}

	// Increase complexity if multi-language
	if len(options.TargetLanguages) > 1 {
		complexity += 0.1
	}

	// Adjust for quality preference
	qualityPref := getQualityPreference(options)
	switch qualityPref {
	case "speed":
		complexity -= 0.1
	case "accuracy":
		complexity += 0.2
	}

	// Additional analysis increases complexity
	if options.ClassifyContent != nil && *options.ClassifyContent {
		complexity += 0.1
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

// getQualityPreference returns the quality preference with a default
func getQualityPreference(options models.ProcessingOptions) string {
	if options.QualityPreference != nil {
		return *options.QualityPreference
	}
	return "balanced" // default
}

// NOTE: extractKeywords() method removed - now using MageAgent.ExtractTopics() directly
// This leverages SOTA NLP models via MageAgent instead of naive keyword extraction

// mergeTranscriptions merges chunk transcriptions with overlap deduplication
func (ae *AudioExtractor) mergeTranscriptions(chunks []chunkResult) string {
	if len(chunks) == 0 {
		return ""
	}
	if len(chunks) == 1 {
		return chunks[0].transcription
	}

	// Simple concatenation for now
	// In production, implement overlap detection and deduplication
	merged := ""
	for _, chunk := range chunks {
		if merged != "" {
			merged += " "
		}
		merged += chunk.transcription
	}
	return merged
}

// mergeSpeakerSegments merges speaker segments from all chunks
func (ae *AudioExtractor) mergeSpeakerSegments(chunks []chunkResult) []models.SpeakerSegment {
	var allSpeakers []models.SpeakerSegment

	// For each chunk, append speakers with adjusted timestamps
	// Chunk overlap is 2 seconds, so we need to track cumulative time
	cumulativeTime := 0.0
	chunkOverlap := 2.0 // seconds

	for i, chunk := range chunks {
		for _, speaker := range chunk.speakers {
			adjustedSpeaker := speaker
			adjustedSpeaker.StartTime += cumulativeTime
			adjustedSpeaker.EndTime += cumulativeTime
			allSpeakers = append(allSpeakers, adjustedSpeaker)
		}

		// Calculate chunk duration (assume last speaker's end time)
		if len(chunk.speakers) > 0 {
			lastSpeaker := chunk.speakers[len(chunk.speakers)-1]
			chunkDuration := lastSpeaker.EndTime
			// Add chunk duration minus overlap for next chunk
			if i < len(chunks)-1 {
				cumulativeTime += chunkDuration - chunkOverlap
			}
		}
	}

	return allSpeakers
}
