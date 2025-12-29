package processor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// BatchFrame represents a frame in a batch with metadata
type BatchFrame struct {
	Frame       *StreamFrame
	StreamKey   string
	ReceivedAt  time.Time
	BatchIndex  int
}

// BatchResult represents the result of processing a batch
type BatchResult struct {
	Results      []StreamResult
	BatchSize    int
	ProcessingMs int64
	Timestamp    time.Time
}

// FrameBatcher collects frames into batches for efficient GPU processing
type FrameBatcher struct {
	maxBatchSize      int
	maxBatchWait      time.Duration
	mageAgentClient   *clients.MageAgentClient
	pendingFrames     []*BatchFrame
	pendingMutex      sync.Mutex
	batchChan         chan []*BatchFrame
	resultChan        chan *BatchResult
	stopChan          chan struct{}
	wg                sync.WaitGroup
	stats             BatcherStats
	statsMutex        sync.RWMutex
	batchWorkers      int
}

// BatcherStats tracks batching statistics
type BatcherStats struct {
	TotalFrames      int64
	TotalBatches     int64
	AverageBatchSize float64
	AverageWaitMs    float64
	AverageProcessMs float64
	LastBatchAt      time.Time
}

// FrameBatcherConfig holds configuration for frame batcher
type FrameBatcherConfig struct {
	MaxBatchSize int           // Default: 16 frames
	MaxBatchWait time.Duration // Default: 50ms
	BatchWorkers int           // Default: 2 concurrent batch processors
}

// NewFrameBatcher creates a new frame batcher
func NewFrameBatcher(config FrameBatcherConfig, mageAgentClient *clients.MageAgentClient) *FrameBatcher {
	// Set defaults
	if config.MaxBatchSize == 0 {
		config.MaxBatchSize = 16
	}
	if config.MaxBatchWait == 0 {
		config.MaxBatchWait = 50 * time.Millisecond
	}
	if config.BatchWorkers == 0 {
		config.BatchWorkers = 2
	}

	return &FrameBatcher{
		maxBatchSize:    config.MaxBatchSize,
		maxBatchWait:    config.MaxBatchWait,
		mageAgentClient: mageAgentClient,
		pendingFrames:   make([]*BatchFrame, 0, config.MaxBatchSize),
		batchChan:       make(chan []*BatchFrame, 10), // Buffer up to 10 batches
		resultChan:      make(chan *BatchResult, 100), // Buffer up to 100 results
		stopChan:        make(chan struct{}),
		batchWorkers:    config.BatchWorkers,
		stats: BatcherStats{
			LastBatchAt: time.Now(),
		},
	}
}

// Start begins the frame batcher
func (fb *FrameBatcher) Start(ctx context.Context) error {
	log.Printf("FrameBatcher: Starting with batch size %d, wait time %v, workers %d",
		fb.maxBatchSize, fb.maxBatchWait, fb.batchWorkers)

	// Start batch timeout ticker
	fb.wg.Add(1)
	go fb.batchTimeoutWatcher(ctx)

	// Start batch workers
	for i := 0; i < fb.batchWorkers; i++ {
		fb.wg.Add(1)
		go fb.batchWorker(ctx, i)
	}

	// Start stats logger
	fb.wg.Add(1)
	go fb.statsLogger(ctx)

	log.Println("FrameBatcher: Started successfully")
	return nil
}

// Stop gracefully stops the frame batcher
func (fb *FrameBatcher) Stop() error {
	log.Println("FrameBatcher: Stopping...")
	close(fb.stopChan)

	// Flush any pending frames
	fb.flushPendingFrames()

	// Wait for all workers to finish
	fb.wg.Wait()

	// Close channels
	close(fb.batchChan)
	close(fb.resultChan)

	log.Println("FrameBatcher: Stopped successfully")
	return nil
}

// AddFrame adds a frame to the batch
func (fb *FrameBatcher) AddFrame(frame *StreamFrame, streamKey string) error {
	fb.pendingMutex.Lock()
	defer fb.pendingMutex.Unlock()

	// Create batch frame
	batchFrame := &BatchFrame{
		Frame:      frame,
		StreamKey:  streamKey,
		ReceivedAt: time.Now(),
		BatchIndex: len(fb.pendingFrames),
	}

	// Add to pending frames
	fb.pendingFrames = append(fb.pendingFrames, batchFrame)

	// If batch is full, flush immediately
	if len(fb.pendingFrames) >= fb.maxBatchSize {
		fb.flushPendingFramesLocked()
	}

	return nil
}

// GetResultChan returns the result channel
func (fb *FrameBatcher) GetResultChan() <-chan *BatchResult {
	return fb.resultChan
}

// batchTimeoutWatcher watches for batch timeout and flushes pending frames
func (fb *FrameBatcher) batchTimeoutWatcher(ctx context.Context) {
	defer fb.wg.Done()

	ticker := time.NewTicker(fb.maxBatchWait)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fb.stopChan:
			return
		case <-ticker.C:
			fb.flushPendingFrames()
		}
	}
}

// flushPendingFrames flushes pending frames to batch channel
func (fb *FrameBatcher) flushPendingFrames() {
	fb.pendingMutex.Lock()
	defer fb.pendingMutex.Unlock()

	fb.flushPendingFramesLocked()
}

// flushPendingFramesLocked flushes pending frames (must hold pendingMutex)
func (fb *FrameBatcher) flushPendingFramesLocked() {
	if len(fb.pendingFrames) == 0 {
		return
	}

	// Create batch copy
	batch := make([]*BatchFrame, len(fb.pendingFrames))
	copy(batch, fb.pendingFrames)

	// Send to batch channel (non-blocking)
	select {
	case fb.batchChan <- batch:
		log.Printf("FrameBatcher: Flushed batch of %d frames", len(batch))
	default:
		log.Println("FrameBatcher: Warning - batch channel full, dropping batch")
	}

	// Reset pending frames
	fb.pendingFrames = fb.pendingFrames[:0]

	// Update stats
	fb.statsMutex.Lock()
	fb.stats.TotalBatches++
	fb.stats.LastBatchAt = time.Now()
	fb.statsMutex.Unlock()
}

// batchWorker processes batches of frames
func (fb *FrameBatcher) batchWorker(ctx context.Context, workerID int) {
	defer fb.wg.Done()

	log.Printf("FrameBatcher: Worker %d started", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("FrameBatcher: Worker %d stopping (context done)", workerID)
			return
		case <-fb.stopChan:
			log.Printf("FrameBatcher: Worker %d stopping (stop signal)", workerID)
			return
		case batch, ok := <-fb.batchChan:
			if !ok {
				log.Printf("FrameBatcher: Worker %d stopping (channel closed)", workerID)
				return
			}

			// Process batch
			if err := fb.processBatch(ctx, batch, workerID); err != nil {
				log.Printf("FrameBatcher: Worker %d failed to process batch: %v", workerID, err)
			}
		}
	}
}

// processBatch processes a batch of frames
func (fb *FrameBatcher) processBatch(ctx context.Context, batch []*BatchFrame, workerID int) error {
	startTime := time.Now()
	batchSize := len(batch)

	log.Printf("FrameBatcher: Worker %d processing batch of %d frames", workerID, batchSize)

	// Process each frame in the batch
	// TODO: Implement true batched GPU inference here
	// For now, process frames individually but in parallel
	results := make([]StreamResult, 0, batchSize)
	resultsMutex := sync.Mutex{}
	wg := sync.WaitGroup{}

	for _, batchFrame := range batch {
		wg.Add(1)
		go func(bf *BatchFrame) {
			defer wg.Done()

			// Process individual frame
			result, err := fb.processFrameInBatch(ctx, bf)
			if err != nil {
				log.Printf("FrameBatcher: Worker %d failed to process frame %d: %v",
					workerID, bf.Frame.FrameNumber, err)
				return
			}

			// Add result
			resultsMutex.Lock()
			results = append(results, *result)
			resultsMutex.Unlock()
		}(batchFrame)
	}

	// Wait for all frames to be processed
	wg.Wait()

	processingMs := time.Since(startTime).Milliseconds()

	// Create batch result
	batchResult := &BatchResult{
		Results:      results,
		BatchSize:    batchSize,
		ProcessingMs: processingMs,
		Timestamp:    time.Now(),
	}

	// Send to result channel
	select {
	case fb.resultChan <- batchResult:
		log.Printf("FrameBatcher: Worker %d completed batch of %d frames in %dms (avg %.1fms/frame)",
			workerID, batchSize, processingMs, float64(processingMs)/float64(batchSize))
	default:
		log.Println("FrameBatcher: Warning - result channel full, dropping batch result")
	}

	// Update stats
	fb.statsMutex.Lock()
	fb.stats.TotalFrames += int64(batchSize)
	totalBatches := fb.stats.TotalBatches
	fb.stats.AverageBatchSize = float64(fb.stats.TotalFrames) / float64(totalBatches)
	fb.stats.AverageProcessMs = (fb.stats.AverageProcessMs*float64(totalBatches-1) + float64(processingMs)) / float64(totalBatches)
	fb.statsMutex.Unlock()

	return nil
}

// processFrameInBatch processes a single frame within a batch
func (fb *FrameBatcher) processFrameInBatch(ctx context.Context, batchFrame *BatchFrame) (*StreamResult, error) {
	frame := batchFrame.Frame

	// Prepare vision request
	visionReq := models.MageAgentVisionRequest{
		Image:     frame.FrameData, // Base64 encoded
		Prompt:    "Analyze this video frame in the context of a video stream.",
		MaxTokens: 300, // Shorter for batched processing
	}

	// Call MageAgent for vision analysis
	visionResp, err := fb.mageAgentClient.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Create result
	result := &StreamResult{
		StreamID:    batchFrame.StreamKey,
		ClientID:    frame.ClientID,
		FrameNumber: frame.FrameNumber,
		Timestamp:   time.Now().UnixMilli(),
		Type:        "vision",
		Data:        visionResp,
	}

	return result, nil
}

// statsLogger periodically logs statistics
func (fb *FrameBatcher) statsLogger(ctx context.Context) {
	defer fb.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fb.stopChan:
			return
		case <-ticker.C:
			fb.logStats()
		}
	}
}

// logStats logs current statistics
func (fb *FrameBatcher) logStats() {
	fb.statsMutex.RLock()
	defer fb.statsMutex.RUnlock()

	log.Printf("FrameBatcher Stats: TotalFrames=%d, TotalBatches=%d, AvgBatchSize=%.1f, AvgProcessMs=%.1f, LastBatch=%v",
		fb.stats.TotalFrames,
		fb.stats.TotalBatches,
		fb.stats.AverageBatchSize,
		fb.stats.AverageProcessMs,
		fb.stats.LastBatchAt.Format(time.RFC3339),
	)
}

// GetStats returns current batching statistics
func (fb *FrameBatcher) GetStats() BatcherStats {
	fb.statsMutex.RLock()
	defer fb.statsMutex.RUnlock()
	return fb.stats
}
