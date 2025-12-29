package processor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ResultType represents the type of progressive result
type ResultType string

const (
	ResultTypePartial ResultType = "partial" // Initial low-confidence result
	ResultTypeRefined ResultType = "refined" // Improved result after analysis
	ResultTypeFinal   ResultType = "final"   // Complete result with metadata
)

// ProgressiveResult represents a result at different stages of refinement
type ProgressiveResult struct {
	StreamID       string                 `json:"streamId"`
	ClientID       string                 `json:"clientId"`
	FrameNumber    int                    `json:"frameNumber"`
	Timestamp      int64                  `json:"timestamp"`
	Type           string                 `json:"type"` // "vision", "transcription", etc.
	ResultType     ResultType             `json:"resultType"` // "partial", "refined", "final"
	Data           interface{}            `json:"data"`
	Confidence     float64                `json:"confidence,omitempty"` // 0.0-1.0
	ProcessingTime int64                  `json:"processingTime"` // Milliseconds
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

// ProgressiveResultsHandler manages progressive result delivery
type ProgressiveResultsHandler struct {
	redisClient     *redis.Client
	pendingResults  map[string]*ProgressiveResultState // Key: frameID (streamID:frameNumber)
	resultsMutex    sync.RWMutex
	stopChan        chan struct{}
	wg              sync.WaitGroup
	stats           ProgressiveStats
	statsMutex      sync.RWMutex
	refinementDelay time.Duration // Delay before sending refined result
	finalDelay      time.Duration // Delay before sending final result
}

// ProgressiveResultState tracks state of a result through refinement stages
type ProgressiveResultState struct {
	StreamID       string
	ClientID       string
	FrameNumber    int
	PartialSent    bool
	PartialTime    time.Time
	RefinedSent    bool
	RefinedTime    time.Time
	FinalSent      bool
	FinalTime      time.Time
	BaseResult     *StreamResult
	EnrichedData   map[string]interface{}
	CreatedAt      time.Time
}

// ProgressiveStats tracks progressive result statistics
type ProgressiveStats struct {
	TotalPartial  int64
	TotalRefined  int64
	TotalFinal    int64
	AvgTimeToPartialMs  float64
	AvgTimeToRefinedMs  float64
	AvgTimeToFinalMs    float64
}

// ProgressiveResultsConfig holds configuration for progressive results
type ProgressiveResultsConfig struct {
	RefinementDelay time.Duration // Default: 500ms
	FinalDelay      time.Duration // Default: 1500ms
	RedisURL        string
}

// NewProgressiveResultsHandler creates a new progressive results handler
func NewProgressiveResultsHandler(config ProgressiveResultsConfig) (*ProgressiveResultsHandler, error) {
	// Set defaults
	if config.RefinementDelay == 0 {
		config.RefinementDelay = 500 * time.Millisecond
	}
	if config.FinalDelay == 0 {
		config.FinalDelay = 1500 * time.Millisecond
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

	return &ProgressiveResultsHandler{
		redisClient:     redisClient,
		pendingResults:  make(map[string]*ProgressiveResultState),
		stopChan:        make(chan struct{}),
		refinementDelay: config.RefinementDelay,
		finalDelay:      config.FinalDelay,
	}, nil
}

// Start begins the progressive results handler
func (prh *ProgressiveResultsHandler) Start(ctx context.Context) error {
	log.Printf("ProgressiveResultsHandler: Starting with refinement delay %v, final delay %v",
		prh.refinementDelay, prh.finalDelay)

	// Start refinement worker
	prh.wg.Add(1)
	go prh.refinementWorker(ctx)

	// Start stats logger
	prh.wg.Add(1)
	go prh.statsLogger(ctx)

	log.Println("ProgressiveResultsHandler: Started successfully")
	return nil
}

// Stop gracefully stops the progressive results handler
func (prh *ProgressiveResultsHandler) Stop() error {
	log.Println("ProgressiveResultsHandler: Stopping...")
	close(prh.stopChan)

	// Wait for all workers to finish
	prh.wg.Wait()

	// Close Redis connection
	if err := prh.redisClient.Close(); err != nil {
		log.Printf("ProgressiveResultsHandler: Error closing Redis connection: %v", err)
	}

	log.Println("ProgressiveResultsHandler: Stopped successfully")
	return nil
}

// HandleResult processes a result through progressive stages
func (prh *ProgressiveResultsHandler) HandleResult(ctx context.Context, result *StreamResult) error {
	frameID := fmt.Sprintf("%s:%d", result.StreamID, result.FrameNumber)

	prh.resultsMutex.Lock()
	state, exists := prh.pendingResults[frameID]
	if !exists {
		// Create new state
		state = &ProgressiveResultState{
			StreamID:     result.StreamID,
			ClientID:     result.ClientID,
			FrameNumber:  result.FrameNumber,
			BaseResult:   result,
			EnrichedData: make(map[string]interface{}),
			CreatedAt:    time.Now(),
		}
		prh.pendingResults[frameID] = state
	}
	prh.resultsMutex.Unlock()

	// Send partial result immediately (low confidence)
	if !state.PartialSent {
		if err := prh.sendPartialResult(ctx, state); err != nil {
			log.Printf("ProgressiveResultsHandler: Failed to send partial result: %v", err)
		}
	}

	return nil
}

// sendPartialResult sends initial low-confidence result
func (prh *ProgressiveResultsHandler) sendPartialResult(ctx context.Context, state *ProgressiveResultState) error {
	result := &ProgressiveResult{
		StreamID:       state.StreamID,
		ClientID:       state.ClientID,
		FrameNumber:    state.FrameNumber,
		Timestamp:      time.Now().UnixMilli(),
		Type:           state.BaseResult.Type,
		ResultType:     ResultTypePartial,
		Data:           state.BaseResult.Data,
		Confidence:     0.6, // Low confidence for partial results
		ProcessingTime: state.BaseResult.ProcessingTime,
		Metadata: map[string]interface{}{
			"stage": "partial",
			"note":  "Initial analysis, may be refined",
		},
	}

	if err := prh.publishResult(ctx, result); err != nil {
		return err
	}

	// Update state
	prh.resultsMutex.Lock()
	state.PartialSent = true
	state.PartialTime = time.Now()
	prh.resultsMutex.Unlock()

	// Update stats
	prh.statsMutex.Lock()
	prh.stats.TotalPartial++
	timeToPartial := float64(time.Since(state.CreatedAt).Milliseconds())
	prh.stats.AvgTimeToPartialMs = (prh.stats.AvgTimeToPartialMs*float64(prh.stats.TotalPartial-1) + timeToPartial) / float64(prh.stats.TotalPartial)
	prh.statsMutex.Unlock()

	log.Printf("ProgressiveResultsHandler: Sent partial result for frame %d (confidence: %.1f)",
		state.FrameNumber, result.Confidence)

	return nil
}

// sendRefinedResult sends improved result after analysis
func (prh *ProgressiveResultsHandler) sendRefinedResult(ctx context.Context, state *ProgressiveResultState) error {
	// Simulate refinement with slightly higher confidence
	// In production, this would involve additional processing
	result := &ProgressiveResult{
		StreamID:       state.StreamID,
		ClientID:       state.ClientID,
		FrameNumber:    state.FrameNumber,
		Timestamp:      time.Now().UnixMilli(),
		Type:           state.BaseResult.Type,
		ResultType:     ResultTypeRefined,
		Data:           state.BaseResult.Data,
		Confidence:     0.85, // Higher confidence for refined results
		ProcessingTime: state.BaseResult.ProcessingTime + time.Since(state.PartialTime).Milliseconds(),
		Metadata: map[string]interface{}{
			"stage":                "refined",
			"note":                 "Refined analysis with additional context",
			"refinement_time_ms":   time.Since(state.PartialTime).Milliseconds(),
		},
	}

	if err := prh.publishResult(ctx, result); err != nil {
		return err
	}

	// Update state
	prh.resultsMutex.Lock()
	state.RefinedSent = true
	state.RefinedTime = time.Now()
	prh.resultsMutex.Unlock()

	// Update stats
	prh.statsMutex.Lock()
	prh.stats.TotalRefined++
	timeToRefined := float64(time.Since(state.CreatedAt).Milliseconds())
	prh.stats.AvgTimeToRefinedMs = (prh.stats.AvgTimeToRefinedMs*float64(prh.stats.TotalRefined-1) + timeToRefined) / float64(prh.stats.TotalRefined)
	prh.statsMutex.Unlock()

	log.Printf("ProgressiveResultsHandler: Sent refined result for frame %d (confidence: %.1f)",
		state.FrameNumber, result.Confidence)

	return nil
}

// sendFinalResult sends complete result with enriched metadata
func (prh *ProgressiveResultsHandler) sendFinalResult(ctx context.Context, state *ProgressiveResultState) error {
	// Final result includes all enriched data
	result := &ProgressiveResult{
		StreamID:       state.StreamID,
		ClientID:       state.ClientID,
		FrameNumber:    state.FrameNumber,
		Timestamp:      time.Now().UnixMilli(),
		Type:           state.BaseResult.Type,
		ResultType:     ResultTypeFinal,
		Data:           state.BaseResult.Data,
		Confidence:     0.95, // Highest confidence for final results
		ProcessingTime: state.BaseResult.ProcessingTime + time.Since(state.PartialTime).Milliseconds(),
		Metadata: map[string]interface{}{
			"stage":                "final",
			"note":                 "Complete analysis with full metadata",
			"total_time_ms":        time.Since(state.CreatedAt).Milliseconds(),
			"partial_time_ms":      state.PartialTime.Sub(state.CreatedAt).Milliseconds(),
			"refined_time_ms":      state.RefinedTime.Sub(state.PartialTime).Milliseconds(),
			"final_time_ms":        time.Since(state.RefinedTime).Milliseconds(),
			"enriched_data":        state.EnrichedData,
		},
	}

	if err := prh.publishResult(ctx, result); err != nil {
		return err
	}

	// Update state
	prh.resultsMutex.Lock()
	state.FinalSent = true
	state.FinalTime = time.Now()
	// Remove from pending results (cleanup)
	delete(prh.pendingResults, fmt.Sprintf("%s:%d", state.StreamID, state.FrameNumber))
	prh.resultsMutex.Unlock()

	// Update stats
	prh.statsMutex.Lock()
	prh.stats.TotalFinal++
	timeToFinal := float64(time.Since(state.CreatedAt).Milliseconds())
	prh.stats.AvgTimeToFinalMs = (prh.stats.AvgTimeToFinalMs*float64(prh.stats.TotalFinal-1) + timeToFinal) / float64(prh.stats.TotalFinal)
	prh.statsMutex.Unlock()

	log.Printf("ProgressiveResultsHandler: Sent final result for frame %d (confidence: %.1f, total time: %dms)",
		state.FrameNumber, result.Confidence, time.Since(state.CreatedAt).Milliseconds())

	return nil
}

// refinementWorker processes pending results through refinement stages
func (prh *ProgressiveResultsHandler) refinementWorker(ctx context.Context) {
	defer prh.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond) // Check every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-prh.stopChan:
			return
		case <-ticker.C:
			prh.processPendingRefinements(ctx)
		}
	}
}

// processPendingRefinements processes pending results that need refinement
func (prh *ProgressiveResultsHandler) processPendingRefinements(ctx context.Context) {
	prh.resultsMutex.RLock()
	statesToProcess := make([]*ProgressiveResultState, 0, len(prh.pendingResults))
	for _, state := range prh.pendingResults {
		statesToProcess = append(statesToProcess, state)
	}
	prh.resultsMutex.RUnlock()

	now := time.Now()

	for _, state := range statesToProcess {
		// Send refined result if enough time has passed
		if state.PartialSent && !state.RefinedSent {
			if now.Sub(state.PartialTime) >= prh.refinementDelay {
				if err := prh.sendRefinedResult(ctx, state); err != nil {
					log.Printf("ProgressiveResultsHandler: Failed to send refined result: %v", err)
				}
			}
		}

		// Send final result if enough time has passed
		if state.RefinedSent && !state.FinalSent {
			if now.Sub(state.RefinedTime) >= prh.finalDelay {
				if err := prh.sendFinalResult(ctx, state); err != nil {
					log.Printf("ProgressiveResultsHandler: Failed to send final result: %v", err)
				}
			}
		}
	}
}

// publishResult publishes a progressive result to Redis Streams
func (prh *ProgressiveResultsHandler) publishResult(ctx context.Context, result *ProgressiveResult) error {
	// Publish to videoagent:results stream with result type prefix
	streamKey := fmt.Sprintf("videoagent:results:%s", result.ResultType)

	_, err := prh.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: 10000, // Keep last 10k results per type
		Approx: true,
		Values: map[string]interface{}{
			"streamId":       result.StreamID,
			"clientId":       result.ClientID,
			"frameNumber":    fmt.Sprintf("%d", result.FrameNumber),
			"timestamp":      fmt.Sprintf("%d", result.Timestamp),
			"type":           result.Type,
			"resultType":     string(result.ResultType),
			"confidence":     fmt.Sprintf("%.2f", result.Confidence),
			"processingTime": fmt.Sprintf("%d", result.ProcessingTime),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to publish result: %w", err)
	}

	return nil
}

// statsLogger periodically logs statistics
func (prh *ProgressiveResultsHandler) statsLogger(ctx context.Context) {
	defer prh.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-prh.stopChan:
			return
		case <-ticker.C:
			prh.logStats()
		}
	}
}

// logStats logs current statistics
func (prh *ProgressiveResultsHandler) logStats() {
	prh.statsMutex.RLock()
	stats := prh.stats
	prh.statsMutex.RUnlock()

	prh.resultsMutex.RLock()
	pendingCount := len(prh.pendingResults)
	prh.resultsMutex.RUnlock()

	log.Printf("ProgressiveResultsHandler Stats: Partial=%d, Refined=%d, Final=%d, Pending=%d, AvgTimeToFinal=%.1fms",
		stats.TotalPartial,
		stats.TotalRefined,
		stats.TotalFinal,
		pendingCount,
		stats.AvgTimeToFinalMs,
	)
}

// GetStats returns current progressive results statistics
func (prh *ProgressiveResultsHandler) GetStats() ProgressiveStats {
	prh.statsMutex.RLock()
	defer prh.statsMutex.RUnlock()
	return prh.stats
}
