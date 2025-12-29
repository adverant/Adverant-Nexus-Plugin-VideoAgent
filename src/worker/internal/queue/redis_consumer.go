package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
	"github.com/adverant/nexus/videoagent-worker/internal/processor"
)

// RedisConsumer consumes video processing jobs from Redis queue
type RedisConsumer struct {
	server    *asynq.Server
	processor *processor.VideoProcessor
}

// RedisConsumerConfig holds consumer configuration
type RedisConsumerConfig struct {
	RedisURL    string
	Concurrency int
	Processor   *processor.VideoProcessor
}

// NewRedisConsumer creates a new Redis queue consumer
func NewRedisConsumer(config *RedisConsumerConfig) (*RedisConsumer, error) {
	redisOpt, err := asynq.ParseRedisURI(config.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: config.Concurrency,
			Queues: map[string]int{
				"videoagent:critical": 6,  // High priority
				"videoagent:default":  3,  // Normal priority
				"videoagent:low":      1,  // Low priority
			},
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				// Exponential backoff: 1min, 2min, 4min
				return time.Duration(1<<uint(n)) * time.Minute
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				log.Printf("Task %s failed: %v", task.Type(), err)
			}),
		},
	)

	return &RedisConsumer{
		server:    server,
		processor: config.Processor,
	}, nil
}

// Start starts the consumer
func (rc *RedisConsumer) Start() error {
	mux := asynq.NewServeMux()

	// Register task handler
	mux.HandleFunc("videoagent:process", rc.handleProcessTask)

	log.Println("Starting VideoAgent worker...")

	// Start serving
	if err := rc.server.Run(mux); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}

	return nil
}

// Stop stops the consumer gracefully
func (rc *RedisConsumer) Stop() {
	log.Println("Shutting down VideoAgent worker...")
	rc.server.Shutdown()
}

// handleProcessTask handles video processing tasks
func (rc *RedisConsumer) handleProcessTask(ctx context.Context, task *asynq.Task) error {
	// Parse job payload
	var job models.JobPayload
	if err := json.Unmarshal(task.Payload(), &job); err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}

	log.Printf("Processing job %s (source: %s)", job.JobID, job.SourceType)

	// Process video
	if err := rc.processor.Process(ctx, &job); err != nil {
		log.Printf("Job %s failed: %v", job.JobID, err)
		return err
	}

	log.Printf("Job %s completed successfully", job.JobID)
	return nil
}

// HealthCheck checks if worker is healthy
func (rc *RedisConsumer) HealthCheck() error {
	// Check if server is running
	if rc.server == nil {
		return fmt.Errorf("server not initialized")
	}

	return nil
}
