package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// JobPayload represents the video processing job from TypeScript API
// All optional fields use pointers for JSON unmarshaling compatibility
type JobPayload struct {
	JobID         string                 `json:"jobId"`
	VideoURL      string                 `json:"videoUrl,omitempty"`      // Can be HTTP URL or Google Drive file ID
	VideoBuffer   []byte                 `json:"videoBuffer,omitempty"`   // Base64-encoded video data (for Google Drive streams)
	SourceType    string                 `json:"sourceType,omitempty"`    // "url", "gdrive", "upload"
	UserID        string                 `json:"userId"`
	SessionID     *string                `json:"sessionId,omitempty"`
	Filename      string                 `json:"filename,omitempty"`      // BullMQ compatibility
	OutputDir     string                 `json:"outputDir,omitempty"`     // BullMQ worker sets this
	Options       ProcessingOptions      `json:"options"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	EnqueuedAt    *time.Time             `json:"enqueuedAt,omitempty"`
}

// ProcessingOptions defines what processing to perform
// All boolean fields default to false, all optional fields use pointers
type ProcessingOptions struct {
	// BullMQ-compatible fields (sent by Node.js worker)
	ExtractMetadata *bool `json:"extractMetadata,omitempty"`
	DetectScenes    *bool `json:"detectScenes,omitempty"`
	AnalyzeFrames   *bool `json:"analyzeFrames,omitempty"`
	TranscribeAudio *bool `json:"transcribeAudio,omitempty"`
	MaxFrames       *int  `json:"maxFrames,omitempty"`
	FrameInterval   *int  `json:"frameInterval,omitempty"`

	// Original fields (for backward compatibility)
	ExtractFrames       *bool              `json:"extractFrames,omitempty"`
	FrameSamplingMode   *string            `json:"frameSamplingMode,omitempty"`   // "keyframes", "uniform", "scene-based"
	FrameSampleRate     *int               `json:"frameSampleRate,omitempty"`     // Frames per second to extract
	ExtractAudio        *bool              `json:"extractAudio,omitempty"`
	DetectObjects       *bool              `json:"detectObjects,omitempty"`
	ExtractText         *bool              `json:"extractText,omitempty"`         // OCR
	ClassifyContent     *bool              `json:"classifyContent,omitempty"`
	GenerateSummary     *bool              `json:"generateSummary,omitempty"`
	CustomAnalysis      *string            `json:"customAnalysis,omitempty"`      // Custom prompt for MageAgent
	TargetLanguages     []string           `json:"targetLanguages,omitempty"`     // For transcription (empty = auto-detect)
	QualityPreference   *string            `json:"qualityPreference,omitempty"`   // "speed", "balanced", "accuracy"
	AdditionalMetadata  map[string]string  `json:"additionalMetadata,omitempty"`
}

// Helper methods to get values with defaults
func (o *ProcessingOptions) ShouldExtractMetadata() bool {
	return o.ExtractMetadata != nil && *o.ExtractMetadata
}

func (o *ProcessingOptions) ShouldDetectScenes() bool {
	return o.DetectScenes != nil && *o.DetectScenes
}

func (o *ProcessingOptions) ShouldAnalyzeFrames() bool {
	return o.AnalyzeFrames != nil && *o.AnalyzeFrames
}

func (o *ProcessingOptions) ShouldTranscribeAudio() bool {
	return o.TranscribeAudio != nil && *o.TranscribeAudio
}

func (o *ProcessingOptions) GetMaxFrames() int {
	if o.MaxFrames != nil {
		return *o.MaxFrames
	}
	return 10 // default
}

func (o *ProcessingOptions) GetFrameInterval() int {
	if o.FrameInterval != nil {
		return *o.FrameInterval
	}
	return 1 // default
}

// ProcessingResult is the final result stored in PostgreSQL
type ProcessingResult struct {
	JobID           string                 `json:"jobId"`
	Status          string                 `json:"status"`          // "pending", "processing", "completed", "failed"
	VideoMetadata   VideoMetadata          `json:"videoMetadata"`
	Frames          []FrameAnalysis        `json:"frames"`
	AudioAnalysis   *AudioAnalysis         `json:"audioAnalysis,omitempty"`
	Scenes          []SceneDetection       `json:"scenes"`
	Objects         []ObjectDetection      `json:"objects"`
	TextExtraction  []TextExtraction       `json:"textExtraction"`
	Classification  *ContentClassification `json:"classification,omitempty"`
	Summary         string                 `json:"summary"`
	Error           string                 `json:"error,omitempty"`
	ProcessingTime  float64                `json:"processingTime"`  // Seconds
	ModelUsage      []ModelUsageRecord     `json:"modelUsage"`      // Track which models were used
	StartedAt       time.Time              `json:"startedAt"`
	CompletedAt     time.Time              `json:"completedAt"`
}

// VideoMetadata contains technical video information
type VideoMetadata struct {
	Duration    float64 `json:"duration"`    // Seconds
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	FrameRate   float64 `json:"frameRate"`
	Codec       string  `json:"codec"`
	Bitrate     int64   `json:"bitrate"`
	Size        int64   `json:"size"`        // Bytes
	Format      string  `json:"format"`
	AudioCodec  string  `json:"audioCodec"`
	AudioTracks int     `json:"audioTracks"`
	HasSubtitles bool   `json:"hasSubtitles"`
	Quality     string  `json:"quality"`     // "low", "medium", "high", "4k"
}

// FrameAnalysis contains AI analysis of a single frame
type FrameAnalysis struct {
	FrameID     string                 `json:"frameId"`
	Timestamp   float64                `json:"timestamp"`   // Seconds from start
	FrameNumber int                    `json:"frameNumber"`
	FilePath    string                 `json:"filePath"`    // Local storage path
	Objects     []ObjectDetection      `json:"objects"`
	Text        []TextExtraction       `json:"text"`
	Description string                 `json:"description"` // AI-generated description
	Embedding   []float32              `json:"embedding"`   // Vector embedding for similarity search
	ModelUsed   string                 `json:"modelUsed"`   // Which model analyzed this frame
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// AudioAnalysis contains transcription and audio analysis
type AudioAnalysis struct {
	Transcription   string                 `json:"transcription"`
	Language        string                 `json:"language"`
	Confidence      float64                `json:"confidence"`
	Speakers        []SpeakerSegment       `json:"speakers"`       // Speaker diarization
	Sentiment       string                 `json:"sentiment"`      // "positive", "negative", "neutral"
	Topics          []string               `json:"topics"`
	Keywords        []string               `json:"keywords"`
	AudioFilePath   string                 `json:"audioFilePath"`  // Extracted audio file
	ModelUsed       string                 `json:"modelUsed"`
	ProcessingTime  float64                `json:"processingTime"`
}

// SpeakerSegment represents a diarized speaker segment
type SpeakerSegment struct {
	SpeakerID   string  `json:"speakerId"`
	StartTime   float64 `json:"startTime"`
	EndTime     float64 `json:"endTime"`
	Text        string  `json:"text"`
	Confidence  float64 `json:"confidence"`
}

// SceneDetection represents a detected scene
type SceneDetection struct {
	SceneID       string  `json:"sceneId"`
	StartTime     float64 `json:"startTime"`
	EndTime       float64 `json:"endTime"`
	StartFrame    int     `json:"startFrame"`
	EndFrame      int     `json:"endFrame"`
	Description   string  `json:"description"`
	KeyFrameID    string  `json:"keyFrameId"`    // Reference to FrameAnalysis
	SceneType     string  `json:"sceneType"`     // "action", "dialogue", "establishing", etc.
	Confidence    float64 `json:"confidence"`
}

// ObjectDetection represents a detected object in a frame
type ObjectDetection struct {
	ObjectID    string    `json:"objectId"`
	Label       string    `json:"label"`
	Confidence  float64   `json:"confidence"`
	BoundingBox BoundingBox `json:"boundingBox"`
	Timestamp   float64   `json:"timestamp,omitempty"` // For video-level objects
}

// BoundingBox defines object location in frame
type BoundingBox struct {
	X      float64 `json:"x"`      // Normalized 0-1
	Y      float64 `json:"y"`      // Normalized 0-1
	Width  float64 `json:"width"`  // Normalized 0-1
	Height float64 `json:"height"` // Normalized 0-1
}

// TextExtraction represents OCR results
type TextExtraction struct {
	TextID      string      `json:"textId"`
	Text        string      `json:"text"`
	Confidence  float64     `json:"confidence"`
	BoundingBox BoundingBox `json:"boundingBox"`
	Language    string      `json:"language"`
	Timestamp   float64     `json:"timestamp,omitempty"`
}

// ContentClassification contains AI classification results
type ContentClassification struct {
	PrimaryCategory string            `json:"primaryCategory"`
	Categories      map[string]float64 `json:"categories"`     // Category -> confidence
	Tags            []string          `json:"tags"`
	ContentRating   string            `json:"contentRating"`  // "G", "PG", "PG-13", "R"
	IsNSFW          bool              `json:"isNsfw"`
	Confidence      float64           `json:"confidence"`
	ModelUsed       string            `json:"modelUsed"`
}

// ModelUsageRecord tracks which AI models were used
type ModelUsageRecord struct {
	TaskType      string    `json:"taskType"`      // "frame_analysis", "transcription", "classification"
	ModelID       string    `json:"modelId"`       // From MageAgent
	ModelProvider string    `json:"modelProvider"` // "openai", "anthropic", "google"
	Complexity    float64   `json:"complexity"`    // 0-1 scale
	Cost          float64   `json:"cost"`          // USD
	Duration      float64   `json:"duration"`      // Seconds
	Success       bool      `json:"success"`
	Timestamp     time.Time `json:"timestamp"`
}

// ProgressUpdate for WebSocket real-time updates
type ProgressUpdate struct {
	JobID           string    `json:"jobId"`
	Status          string    `json:"status"`
	Progress        float64   `json:"progress"`        // 0-100
	CurrentStage    string    `json:"currentStage"`
	Message         string    `json:"message"`
	FramesProcessed int       `json:"framesProcessed"`
	TotalFrames     int       `json:"totalFrames"`
	ElapsedTime     float64   `json:"elapsedTime"`
	EstimatedRemaining float64 `json:"estimatedRemaining"`
	Timestamp       time.Time `json:"timestamp"`
}

// MageAgentModelRequest for dynamic model selection (zero hardcoded models)
type MageAgentModelRequest struct {
	TaskType   string                 `json:"taskType"`   // "vision", "transcription", "classification"
	Complexity float64                `json:"complexity"` // 0-1 scale
	Context    map[string]interface{} `json:"context"`
	Budget     float64                `json:"budget,omitempty"` // Max cost in USD
}

// MageAgentModelResponse from brain_model_select
type MageAgentModelResponse struct {
	ModelID       string  `json:"modelId"`
	ModelProvider string  `json:"modelProvider"`
	EstimatedCost float64 `json:"estimatedCost"`
	Reasoning     string  `json:"reasoning"`
}

// MageAgentVisionRequest for frame analysis
type MageAgentVisionRequest struct {
	Image          string                 `json:"image"`          // Base64-encoded
	Prompt         string                 `json:"prompt"`
	ModelID        string                 `json:"modelId"`        // From model selection
	MaxTokens      int                    `json:"maxTokens"`
	AdditionalContext map[string]interface{} `json:"additionalContext,omitempty"`
}

// MageAgentVisionResponseRaw handles the actual response format from MageAgent Vision API
// MageAgent currently returns plain text descriptions instead of structured data
type MageAgentVisionResponseRaw struct {
	Description string                 `json:"description"`
	Objects     interface{}            `json:"objects"` // Can be string or []ObjectDetection
	Text        interface{}            `json:"text"`    // Can be string or []TextExtraction
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// MageAgentVisionResponse is the normalized, structured response after parsing
type MageAgentVisionResponse struct {
	Description string                 `json:"description"`
	Objects     []ObjectDetection      `json:"objects"`
	Text        []TextExtraction       `json:"text"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// NormalizeResponse converts raw MageAgent response to structured format
// Handles both string and array formats for Objects and Text fields
func (r *MageAgentVisionResponseRaw) NormalizeResponse() (*MageAgentVisionResponse, error) {
	response := &MageAgentVisionResponse{
		Description: r.Description,
		Confidence:  r.Confidence,
		Metadata:    r.Metadata,
	}

	// Parse Objects field (can be string or []ObjectDetection)
	switch v := r.Objects.(type) {
	case string:
		// Text format: "dog, cat, tree" or "A dog at (0.1,0.2,0.5,0.6), a cat"
		if v != "" && v != "none" && v != "None" {
			response.Objects = parseObjectsFromString(v)
		} else {
			response.Objects = []ObjectDetection{}
		}
	case []interface{}:
		// Array format (future MageAgent enhancement)
		for _, obj := range v {
			if objMap, ok := obj.(map[string]interface{}); ok {
				response.Objects = append(response.Objects, parseObjectFromMap(objMap))
			}
		}
	case nil:
		response.Objects = []ObjectDetection{}
	default:
		// Try to marshal and unmarshal as structured data
		jsonData, _ := json.Marshal(v)
		var objects []ObjectDetection
		if err := json.Unmarshal(jsonData, &objects); err == nil {
			response.Objects = objects
		} else {
			response.Objects = []ObjectDetection{}
		}
	}

	// Parse Text field (can be string or []TextExtraction)
	switch v := r.Text.(type) {
	case string:
		if v != "" && v != "none" && v != "None" && v != "No text detected" {
			response.Text = parseTextFromString(v)
		} else {
			response.Text = []TextExtraction{}
		}
	case []interface{}:
		// Array format (future enhancement)
		for _, txt := range v {
			if txtMap, ok := txt.(map[string]interface{}); ok {
				response.Text = append(response.Text, parseTextFromMap(txtMap))
			}
		}
	case nil:
		response.Text = []TextExtraction{}
	default:
		// Try to marshal and unmarshal as structured data
		jsonData, _ := json.Marshal(v)
		var texts []TextExtraction
		if err := json.Unmarshal(jsonData, &texts); err == nil {
			response.Text = texts
		} else {
			response.Text = []TextExtraction{}
		}
	}

	return response, nil
}

// parseObjectsFromString parses comma-separated object descriptions
// Format: "dog, cat, tree" or "dog at (0.1,0.2,0.3,0.4), cat"
func parseObjectsFromString(s string) []ObjectDetection {
	var objects []ObjectDetection

	// Split by comma
	parts := strings.Split(s, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		obj := ObjectDetection{
			ObjectID:   NewObjectID(),
			Confidence: 0.5, // Default confidence for text-based detection
		}

		// Try to extract bounding box if present: "dog at (0.1,0.2,0.3,0.4)"
		if idx := strings.Index(part, " at ("); idx != -1 {
			obj.Label = strings.TrimSpace(part[:idx])
			coordStr := part[idx+5:] // Skip " at ("
			if endIdx := strings.Index(coordStr, ")"); endIdx != -1 {
				coordStr = coordStr[:endIdx]
				coords := strings.Split(coordStr, ",")
				if len(coords) == 4 {
					obj.BoundingBox = BoundingBox{
						X:      parseFloatSafe(coords[0]),
						Y:      parseFloatSafe(coords[1]),
						Width:  parseFloatSafe(coords[2]),
						Height: parseFloatSafe(coords[3]),
					}
				}
			}
		} else {
			// No bounding box, just label
			obj.Label = part
			// Create default bounding box (center area)
			obj.BoundingBox = BoundingBox{
				X:      0.25 + float64(i)*0.1, // Offset slightly for each object
				Y:      0.25,
				Width:  0.3,
				Height: 0.3,
			}
		}

		objects = append(objects, obj)
	}

	return objects
}

// parseTextFromString parses text description into structured format
func parseTextFromString(s string) []TextExtraction {
	if s == "" {
		return []TextExtraction{}
	}

	// Simple case: treat entire string as one text extraction
	return []TextExtraction{
		{
			TextID:     NewObjectID(),
			Text:       s,
			Confidence: 0.8,
			BoundingBox: BoundingBox{
				X:      0.1,
				Y:      0.1,
				Width:  0.8,
				Height: 0.2,
			},
			Language: "en", // Default
		},
	}
}

// parseObjectFromMap converts map to ObjectDetection
func parseObjectFromMap(m map[string]interface{}) ObjectDetection {
	obj := ObjectDetection{
		ObjectID: NewObjectID(),
	}

	if label, ok := m["label"].(string); ok {
		obj.Label = label
	}
	if conf, ok := m["confidence"].(float64); ok {
		obj.Confidence = conf
	}
	if bbox, ok := m["boundingBox"].(map[string]interface{}); ok {
		obj.BoundingBox = BoundingBox{
			X:      getFloatFromMap(bbox, "x"),
			Y:      getFloatFromMap(bbox, "y"),
			Width:  getFloatFromMap(bbox, "width"),
			Height: getFloatFromMap(bbox, "height"),
		}
	}

	return obj
}

// parseTextFromMap converts map to TextExtraction
func parseTextFromMap(m map[string]interface{}) TextExtraction {
	txt := TextExtraction{
		TextID: NewObjectID(),
	}

	if text, ok := m["text"].(string); ok {
		txt.Text = text
	}
	if conf, ok := m["confidence"].(float64); ok {
		txt.Confidence = conf
	}
	if lang, ok := m["language"].(string); ok {
		txt.Language = lang
	}
	if bbox, ok := m["boundingBox"].(map[string]interface{}); ok {
		txt.BoundingBox = BoundingBox{
			X:      getFloatFromMap(bbox, "x"),
			Y:      getFloatFromMap(bbox, "y"),
			Width:  getFloatFromMap(bbox, "width"),
			Height: getFloatFromMap(bbox, "height"),
		}
	}

	return txt
}

// Helper functions
func parseFloatSafe(s string) float64 {
	s = strings.TrimSpace(s)
	val, _ := strconv.ParseFloat(s, 64)
	return val
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0.0
}

// MageAgentTranscriptionRequest for audio transcription
type MageAgentTranscriptionRequest struct {
	Audio          string   `json:"audio"`          // Base64-encoded WAV
	Language       string   `json:"language"`       // "auto" for detection
	ModelID        string   `json:"modelId"`
	EnableDiarization bool  `json:"enableDiarization"`
	Context        string   `json:"context,omitempty"`
}

// MageAgentTranscriptionResponse from audio analysis
type MageAgentTranscriptionResponse struct {
	Transcription string            `json:"transcription"`
	Language      string            `json:"language"`
	Confidence    float64           `json:"confidence"`
	Speakers      []SpeakerSegment  `json:"speakers"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// TaskSubmitResponse represents async task submission response from MageAgent (Phase 1 async-first)
type TaskSubmitResponse struct {
	Success  bool                   `json:"success"`
	TaskID   string                 `json:"taskId"`
	Status   string                 `json:"status"` // "pending"
	Message  string                 `json:"message"`
	PollURL  string                 `json:"pollUrl"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// MageAgentTaskStatusResponse represents the actual nested response structure
// returned by MageAgent's GET /mageagent/api/tasks/:id endpoint.
//
// Response Structure:
//
//	{
//	  "success": true,
//	  "data": {
//	    "task": {
//	      "id": "...",
//	      "status": "completed|failed|running|pending",
//	      "result": {...},
//	      ...
//	    }
//	  },
//	  "metadata": {...}
//	}
//
// This structure follows MageAgent's generic success envelope pattern.
type MageAgentTaskStatusResponse struct {
	Success  bool                   `json:"success"`
	Data     *TaskDataWrapper       `json:"data,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// TaskDataWrapper wraps the nested "task" object in MageAgent's response.
// This intermediate layer exists because MageAgent returns {"data": {"task": {...}}}
// rather than {"data": {...}} directly.
type TaskDataWrapper struct {
	Task *TaskDetails `json:"task,omitempty"`
}

// TaskDetails contains the actual task status and result information.
// This represents the innermost object in MageAgent's nested response structure.
type TaskDetails struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"` // "completed", "failed", "running", "pending", "timeout"
	Progress    float64                `json:"progress,omitempty"`
	Result      interface{}            `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"createdAt"`
	StartedAt   *time.Time             `json:"startedAt,omitempty"`
	CompletedAt *time.Time             `json:"completedAt,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ValidateAndExtract performs defensive validation of the nested response structure
// and extracts the task details with comprehensive error reporting.
//
// This method implements the Adapter Pattern, converting MageAgent's nested structure
// into a flat, validated TaskDetails object.
//
// Returns:
//   - *TaskDetails: The validated task details
//   - error: Detailed error if response structure is invalid
func (r *MageAgentTaskStatusResponse) ValidateAndExtract() (*TaskDetails, error) {
	// Validation Layer 1: Check success flag
	if !r.Success {
		if r.Error != "" {
			return nil, fmt.Errorf("MageAgent API error: %s", r.Error)
		}
		return nil, fmt.Errorf("MageAgent returned success=false with no error message")
	}

	// Validation Layer 2: Check data wrapper exists
	if r.Data == nil {
		return nil, fmt.Errorf(
			"invalid MageAgent response: 'data' field is null or missing. "+
				"Expected structure: {success: true, data: {task: {...}}}. "+
				"This indicates a malformed API response.",
		)
	}

	// Validation Layer 3: Check task object exists
	if r.Data.Task == nil {
		return nil, fmt.Errorf(
			"invalid MageAgent response: 'data.task' field is null or missing. "+
				"Expected structure: {data: {task: {id: '...', status: '...'}}}. "+
				"Received data object exists but contains no task. "+
				"This may indicate the task was not found or was deleted.",
		)
	}

	task := r.Data.Task

	// Validation Layer 4: Check required fields
	if task.ID == "" {
		return nil, fmt.Errorf(
			"invalid task details: task ID is empty. "+
				"This should never happen and indicates a critical MageAgent bug.",
		)
	}

	if task.Status == "" {
		return nil, fmt.Errorf(
			"invalid task details: task status is empty for task %s. "+
				"Expected one of: completed, failed, running, pending, timeout. "+
				"This indicates MageAgent failed to set task status.",
			task.ID,
		)
	}

	// Validation Layer 5: Validate status is a known value
	validStatuses := map[string]bool{
		"completed":  true,
		"failed":     true,
		"running":    true,
		"pending":    true,
		"timeout":    true,
		"processing": true, // Legacy alias for "running"
	}

	if !validStatuses[task.Status] {
		return nil, fmt.Errorf(
			"invalid task status '%s' for task %s. "+
				"Valid statuses: completed, failed, running, pending, timeout, processing. "+
				"This may indicate MageAgent is using a new status value not yet supported by VideoAgent.",
			task.Status, task.ID,
		)
	}

	// All validations passed - return task details
	return task, nil
}

// IsTerminal returns true if the task is in a terminal state (completed, failed, timeout).
// Terminal states indicate the task will not change further.
func (t *TaskDetails) IsTerminal() bool {
	switch t.Status {
	case "completed", "failed", "timeout":
		return true
	default:
		return false
	}
}

// IsSuccessful returns true if the task completed successfully.
func (t *TaskDetails) IsSuccessful() bool {
	return t.Status == "completed"
}

// GetErrorMessage returns the task's error message if it failed, or empty string if successful.
func (t *TaskDetails) GetErrorMessage() string {
	if t.Status == "failed" || t.Status == "timeout" {
		if t.Error != "" {
			return t.Error
		}
		return fmt.Sprintf("Task failed with status: %s (no error message provided)", t.Status)
	}
	return ""
}

// Legacy type alias for backward compatibility
// DEPRECATED: Use MageAgentTaskStatusResponse instead
type TaskStatusResponse = MageAgentTaskStatusResponse

// Config holds worker configuration
type Config struct {
	RedisURL              string
	PostgresURL           string
	QdrantURL             string
	QdrantCollection      string
	MageAgentURL          string
	GraphRAGURL           string // GraphRAG service for VoyageAI embeddings
	NexusAuthURL          string // Nexus-Auth service for OAuth tokens
	InternalServiceAPIKey string // API key for internal service communication
	WorkerConcurrency     int
	TempDir               string
	MaxVideoSize          int64 // Bytes
	EnableGoogleDrive     bool
}

// UnmarshalJSON implements custom JSON unmarshaling for JobPayload
// Handles Node.js Buffer objects from TypeScript (base64 or Buffer type)
func (p *JobPayload) UnmarshalJSON(data []byte) error {
	type Alias JobPayload
	aux := &struct {
		VideoBuffer interface{} `json:"videoBuffer"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Handle VideoBuffer - can be base64 string or Node.js Buffer object
	if aux.VideoBuffer != nil {
		switch v := aux.VideoBuffer.(type) {
		case string:
			// Base64 string from TypeScript
			// Will be decoded when needed
			p.VideoBuffer = []byte(v) // Store as-is, decode later
		case map[string]interface{}:
			// Legacy Node.js Buffer object format {"type": "Buffer", "data": [...]}
			if bufferType, ok := v["type"].(string); ok && bufferType == "Buffer" {
				if dataArray, ok := v["data"].([]interface{}); ok {
					p.VideoBuffer = make([]byte, len(dataArray))
					for i, val := range dataArray {
						if num, ok := val.(float64); ok {
							p.VideoBuffer[i] = byte(num)
						}
					}
				}
			}
		}
	}

	return nil
}

// NewJobID generates a unique job ID
func NewJobID() string {
	return uuid.New().String()
}

// NewFrameID generates a unique frame ID
func NewFrameID() string {
	return uuid.New().String()
}

// NewObjectID generates a unique object ID
func NewObjectID() string {
	return uuid.New().String()
}

// NewSceneID generates a unique scene ID
func NewSceneID() string {
	return uuid.New().String()
}
