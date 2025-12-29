package processor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/extractor"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// StreamFrame represents an incoming frame from Redis Streams
type StreamFrame struct {
	ClientID    string    `json:"clientId"`
	SessionID   string    `json:"sessionId"`
	UserID      string    `json:"userId"`
	FrameType   string    `json:"frameType"`
	Timestamp   int64     `json:"timestamp"`
	FrameNumber int       `json:"frameNumber"`
	FrameData   string    `json:"frameData"` // Base64 encoded
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Format      string    `json:"format"`
	ReceivedAt  int64     `json:"receivedAt"`
}

// StreamResult represents a processing result to publish
type StreamResult struct {
	StreamID       string      `json:"streamId"`
	ClientID       string      `json:"clientId"`
	FrameNumber    int         `json:"frameNumber"`
	Timestamp      int64       `json:"timestamp"`
	Type           string      `json:"type"` // "vision", "transcription", "classification"
	Data           interface{} `json:"data"`
	ProcessingTime int64       `json:"processingTime"` // Milliseconds
	Error          string      `json:"error,omitempty"`
}

// StreamProcessor handles real-time frame processing from Redis Streams
type StreamProcessor struct {
	redisClient      *redis.Client
	frameExtractor   *extractor.FrameExtractor
	mageAgentClient  *clients.MageAgentClient
	consumerGroup    string
	consumerName     string
	isRunning        bool
	stopChan         chan struct{}
	wg               sync.WaitGroup
	stats            StreamStats
	statsMutex       sync.RWMutex
	maxBatchSize     int
	maxBatchWait     time.Duration
	processingWorkers int
}

// StreamStats tracks processing statistics
type StreamStats struct {
	FramesProcessed   int64
	FramesFailed      int64
	ResultsPublished  int64
	AverageLatencyMs  float64
	TotalProcessingMs int64
	LastProcessedAt   time.Time
}

// StreamProcessorConfig holds configuration for stream processor
type StreamProcessorConfig struct {
	RedisURL          string
	ConsumerGroup     string        // Default: "videoagent-worker"
	ConsumerName      string        // Default: hostname or random ID
	MaxBatchSize      int           // Default: 16 frames
	MaxBatchWait      time.Duration // Default: 50ms
	ProcessingWorkers int           // Default: 4 concurrent workers
}

// NewStreamProcessor creates a new stream processor
func NewStreamProcessor(
	config StreamProcessorConfig,
	frameExtractor *extractor.FrameExtractor,
	mageAgentClient *clients.MageAgentClient,
) (*StreamProcessor, error) {
	// Set defaults
	if config.ConsumerGroup == "" {
		config.ConsumerGroup = "videoagent-worker"
	}
	if config.ConsumerName == "" {
		config.ConsumerName = fmt.Sprintf("worker-%d", time.Now().Unix())
	}
	if config.MaxBatchSize == 0 {
		config.MaxBatchSize = 16
	}
	if config.MaxBatchWait == 0 {
		config.MaxBatchWait = 50 * time.Millisecond
	}
	if config.ProcessingWorkers == 0 {
		config.ProcessingWorkers = 4
	}

	// Connect to Redis
	opts, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	redisClient := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &StreamProcessor{
		redisClient:       redisClient,
		frameExtractor:    frameExtractor,
		mageAgentClient:   mageAgentClient,
		consumerGroup:     config.ConsumerGroup,
		consumerName:      config.ConsumerName,
		stopChan:          make(chan struct{}),
		maxBatchSize:      config.MaxBatchSize,
		maxBatchWait:      config.MaxBatchWait,
		processingWorkers: config.ProcessingWorkers,
		stats: StreamStats{
			LastProcessedAt: time.Now(),
		},
	}, nil
}

// Start begins consuming frames from Redis Streams
func (sp *StreamProcessor) Start(ctx context.Context) error {
	if sp.isRunning {
		return fmt.Errorf("stream processor already running")
	}

	sp.isRunning = true
	log.Printf("StreamProcessor: Starting with %d workers, batch size %d, batch wait %v",
		sp.processingWorkers, sp.maxBatchSize, sp.maxBatchWait)

	// Create consumer group if it doesn't exist
	if err := sp.createConsumerGroup(ctx); err != nil {
		log.Printf("StreamProcessor: Warning - consumer group creation: %v", err)
	}

	// Start processing workers
	for i := 0; i < sp.processingWorkers; i++ {
		sp.wg.Add(1)
		go sp.processingWorker(ctx, i)
	}

	// Start stats logger
	sp.wg.Add(1)
	go sp.statsLogger(ctx)

	log.Println("StreamProcessor: Started successfully")
	return nil
}

// Stop gracefully stops the stream processor
func (sp *StreamProcessor) Stop() error {
	if !sp.isRunning {
		return nil
	}

	log.Println("StreamProcessor: Stopping...")
	sp.isRunning = false
	close(sp.stopChan)

	// Wait for all workers to finish
	sp.wg.Wait()

	// Close Redis connection
	if err := sp.redisClient.Close(); err != nil {
		log.Printf("StreamProcessor: Error closing Redis connection: %v", err)
	}

	log.Println("StreamProcessor: Stopped successfully")
	return nil
}

// createConsumerGroup creates consumer group for all frame streams
func (sp *StreamProcessor) createConsumerGroup(ctx context.Context) error {
	// We'll create consumer groups dynamically as we discover streams
	// For now, just log that we're ready
	log.Printf("StreamProcessor: Consumer group '%s' ready", sp.consumerGroup)
	return nil
}

// processingWorker is a worker goroutine that processes frames
func (sp *StreamProcessor) processingWorker(ctx context.Context, workerID int) {
	defer sp.wg.Done()

	log.Printf("StreamProcessor: Worker %d started", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("StreamProcessor: Worker %d stopping (context done)", workerID)
			return
		case <-sp.stopChan:
			log.Printf("StreamProcessor: Worker %d stopping (stop signal)", workerID)
			return
		default:
			// Read frames from all videoagent:frames:* streams
			if err := sp.consumeFrames(ctx, workerID); err != nil {
				log.Printf("StreamProcessor: Worker %d error: %v", workerID, err)
				time.Sleep(1 * time.Second) // Back off on errors
			}
		}
	}
}

// consumeFrames consumes frames from Redis Streams
func (sp *StreamProcessor) consumeFrames(ctx context.Context, workerID int) error {
	// Use XREADGROUP to consume messages
	// Pattern: videoagent:frames:*
	// We'll scan for all streams matching this pattern

	// Get all frame streams
	streams, err := sp.getFrameStreams(ctx)
	if err != nil {
		return fmt.Errorf("failed to get frame streams: %w", err)
	}

	if len(streams) == 0 {
		// No streams yet, wait a bit
		time.Sleep(1 * time.Second)
		return nil
	}

	// Prepare XREADGROUP arguments
	streamsArgs := make([]string, 0, len(streams)*2)
	for _, stream := range streams {
		// Ensure consumer group exists for this stream
		sp.ensureConsumerGroup(ctx, stream)
		streamsArgs = append(streamsArgs, stream, ">") // ">" means new messages only
	}

	// Read from streams (blocking with 1 second timeout)
	result, err := sp.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    sp.consumerGroup,
		Consumer: sp.consumerName,
		Streams:  streamsArgs,
		Count:    int64(sp.maxBatchSize),
		Block:    1 * time.Second,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			// No messages available
			return nil
		}
		return fmt.Errorf("XREADGROUP failed: %w", err)
	}

	// Process messages from each stream
	for _, stream := range result {
		for _, message := range stream.Messages {
			if err := sp.processMessage(ctx, stream.Stream, message); err != nil {
				log.Printf("StreamProcessor: Worker %d failed to process message %s: %v",
					workerID, message.ID, err)

				sp.statsMutex.Lock()
				sp.stats.FramesFailed++
				sp.statsMutex.Unlock()
			}
		}
	}

	return nil
}

// getFrameStreams gets all frame streams matching videoagent:frames:*
func (sp *StreamProcessor) getFrameStreams(ctx context.Context) ([]string, error) {
	// Use SCAN to find all keys matching pattern
	var streams []string
	iter := sp.redisClient.Scan(ctx, 0, "videoagent:frames:*", 100).Iterator()

	for iter.Next(ctx) {
		streams = append(streams, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return nil, err
	}

	return streams, nil
}

// ensureConsumerGroup ensures consumer group exists for a stream
func (sp *StreamProcessor) ensureConsumerGroup(ctx context.Context, stream string) {
	// Try to create consumer group (ignore error if already exists)
	sp.redisClient.XGroupCreateMkStream(ctx, stream, sp.consumerGroup, "0")
}

// processMessage processes a single message from Redis Streams
func (sp *StreamProcessor) processMessage(ctx context.Context, streamKey string, message redis.XMessage) error {
	startTime := time.Now()

	// Parse message into StreamFrame
	frame, err := sp.parseStreamFrame(message.Values)
	if err != nil {
		return fmt.Errorf("failed to parse frame: %w", err)
	}

	// Process frame
	result, err := sp.processFrame(ctx, frame)
	if err != nil {
		// Publish error result
		errorResult := StreamResult{
			StreamID:       streamKey,
			ClientID:       frame.ClientID,
			FrameNumber:    frame.FrameNumber,
			Timestamp:      time.Now().UnixMilli(),
			Type:           "error",
			ProcessingTime: time.Since(startTime).Milliseconds(),
			Error:          err.Error(),
		}

		if pubErr := sp.publishResult(ctx, errorResult); pubErr != nil {
			log.Printf("StreamProcessor: Failed to publish error result: %v", pubErr)
		}

		return err
	}

	// Publish successful result
	result.StreamID = streamKey
	result.ClientID = frame.ClientID
	result.ProcessingTime = time.Since(startTime).Milliseconds()

	if err := sp.publishResult(ctx, *result); err != nil {
		return fmt.Errorf("failed to publish result: %w", err)
	}

	// Acknowledge message
	if err := sp.redisClient.XAck(ctx, streamKey, sp.consumerGroup, message.ID).Err(); err != nil {
		log.Printf("StreamProcessor: Failed to ACK message %s: %v", message.ID, err)
	}

	// Update stats
	sp.statsMutex.Lock()
	sp.stats.FramesProcessed++
	sp.stats.TotalProcessingMs += time.Since(startTime).Milliseconds()
	sp.stats.AverageLatencyMs = float64(sp.stats.TotalProcessingMs) / float64(sp.stats.FramesProcessed)
	sp.stats.LastProcessedAt = time.Now()
	sp.statsMutex.Unlock()

	return nil
}

// parseStreamFrame parses Redis message values into StreamFrame
func (sp *StreamProcessor) parseStreamFrame(values map[string]interface{}) (*StreamFrame, error) {
	frame := &StreamFrame{}

	// Helper to get string value
	getString := func(key string) string {
		if val, ok := values[key]; ok {
			if str, ok := val.(string); ok {
				return str
			}
		}
		return ""
	}

	// Helper to get int value
	getInt := func(key string) int {
		if val, ok := values[key]; ok {
			if str, ok := val.(string); ok {
				var num int
				fmt.Sscanf(str, "%d", &num)
				return num
			}
		}
		return 0
	}

	// Helper to get int64 value
	getInt64 := func(key string) int64 {
		if val, ok := values[key]; ok {
			if str, ok := val.(string); ok {
				var num int64
				fmt.Sscanf(str, "%d", &num)
				return num
			}
		}
		return 0
	}

	frame.ClientID = getString("clientId")
	frame.SessionID = getString("sessionId")
	frame.UserID = getString("userId")
	frame.FrameType = getString("frameType")
	frame.Timestamp = getInt64("timestamp")
	frame.FrameNumber = getInt("frameNumber")
	frame.FrameData = getString("frameData")
	frame.Width = getInt("width")
	frame.Height = getInt("height")
	frame.Format = getString("format")
	frame.ReceivedAt = getInt64("receivedAt")

	if frame.ClientID == "" || frame.FrameData == "" {
		return nil, fmt.Errorf("invalid frame: missing required fields")
	}

	return frame, nil
}

// processFrame processes a single frame using MageAgent
func (sp *StreamProcessor) processFrame(ctx context.Context, frame *StreamFrame) (*StreamResult, error) {
	// Decode base64 frame data (for validation only)
	_, err := base64.StdEncoding.DecodeString(frame.FrameData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode frame data: %w", err)
	}

	// Prepare vision request
	visionReq := models.MageAgentVisionRequest{
		Image:     frame.FrameData, // MageAgent expects base64
		Prompt:    "Analyze this video frame and describe what you see.",
		MaxTokens: 500,
	}

	// Call MageAgent for vision analysis
	visionResp, err := sp.mageAgentClient.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Create result
	result := &StreamResult{
		FrameNumber: frame.FrameNumber,
		Timestamp:   time.Now().UnixMilli(),
		Type:        "vision",
		Data:        visionResp,
	}

	return result, nil
}

// publishResult publishes processing result to Redis Streams
func (sp *StreamProcessor) publishResult(ctx context.Context, result StreamResult) error {
	// Serialize result to JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	// Publish to videoagent:results stream
	_, err = sp.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: "videoagent:results",
		MaxLen: 10000, // Keep last 10k results
		Approx: true,  // Approximate trimming for performance
		Values: map[string]interface{}{
			"result": string(resultJSON),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to publish result: %w", err)
	}

	sp.statsMutex.Lock()
	sp.stats.ResultsPublished++
	sp.statsMutex.Unlock()

	return nil
}

// statsLogger periodically logs statistics
func (sp *StreamProcessor) statsLogger(ctx context.Context) {
	defer sp.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sp.stopChan:
			return
		case <-ticker.C:
			sp.logStats()
		}
	}
}

// logStats logs current statistics
func (sp *StreamProcessor) logStats() {
	sp.statsMutex.RLock()
	defer sp.statsMutex.RUnlock()

	log.Printf("StreamProcessor Stats: Processed=%d, Failed=%d, Published=%d, AvgLatency=%.2fms, LastProcessed=%v",
		sp.stats.FramesProcessed,
		sp.stats.FramesFailed,
		sp.stats.ResultsPublished,
		sp.stats.AverageLatencyMs,
		sp.stats.LastProcessedAt.Format(time.RFC3339),
	)
}

// GetStats returns current processing statistics
func (sp *StreamProcessor) GetStats() StreamStats {
	sp.statsMutex.RLock()
	defer sp.statsMutex.RUnlock()
	return sp.stats
}
