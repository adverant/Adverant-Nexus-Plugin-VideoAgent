package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// QdrantClient is a placeholder for Qdrant client
// NOTE: Actual vector operations are handled by similarity.QdrantManager
type QdrantClient interface {
	// Placeholder interface for future implementation
}

// StorageManager handles PostgreSQL and Qdrant storage operations
type StorageManager struct {
	db              *sql.DB
	qdrantClient    QdrantClient
	collectionName  string
}

// NewStorageManager creates a new storage manager
func NewStorageManager(postgresURL, qdrantURL, collectionName string) (*StorageManager, error) {
	// Initialize PostgreSQL connection
	db, err := sql.Open("postgres", postgresURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// NOTE: Qdrant client initialization disabled
	// Vector operations are now handled by similarity.QdrantManager
	// This manager only handles PostgreSQL operations
	sm := &StorageManager{
		db:             db,
		qdrantClient:   nil, // Disabled - using similarity.QdrantManager instead
		collectionName: collectionName,
	}

	// Initialize database schema
	if err := sm.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Initialize Qdrant collection (disabled - using similarity.QdrantManager)
	// if err := sm.initQdrantCollection(); err != nil {
	// 	return nil, fmt.Errorf("failed to initialize Qdrant collection: %w", err)
	// }

	return sm, nil
}

// initSchema creates database tables and indexes if they don't exist
// PostgreSQL-native DDL with proper CREATE INDEX statements
func (sm *StorageManager) initSchema() error {
	// Step 1: Create schema and tables
	tableSchema := `
	CREATE SCHEMA IF NOT EXISTS videoagent;

	-- Video processing jobs
	CREATE TABLE IF NOT EXISTS videoagent.jobs (
		job_id VARCHAR(255) PRIMARY KEY,
		user_id VARCHAR(255) NOT NULL,
		session_id VARCHAR(255),
		video_url TEXT,
		source_type VARCHAR(50),
		status VARCHAR(50) NOT NULL,
		options JSONB NOT NULL,
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		error TEXT
	);

	-- Video metadata
	CREATE TABLE IF NOT EXISTS videoagent.video_metadata (
		job_id VARCHAR(255) PRIMARY KEY REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		duration FLOAT,
		width INT,
		height INT,
		frame_rate FLOAT,
		codec VARCHAR(100),
		bitrate BIGINT,
		size BIGINT,
		format VARCHAR(100),
		audio_codec VARCHAR(100),
		audio_tracks INT,
		has_subtitles BOOLEAN,
		quality VARCHAR(50),
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Frame analysis results
	CREATE TABLE IF NOT EXISTS videoagent.frames (
		frame_id VARCHAR(255) PRIMARY KEY,
		job_id VARCHAR(255) NOT NULL REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		timestamp FLOAT NOT NULL,
		frame_number INT NOT NULL,
		file_path TEXT,
		description TEXT,
		confidence FLOAT,
		model_used VARCHAR(255),
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Object detections
	CREATE TABLE IF NOT EXISTS videoagent.objects (
		object_id VARCHAR(255) PRIMARY KEY,
		frame_id VARCHAR(255) REFERENCES videoagent.frames(frame_id) ON DELETE CASCADE,
		job_id VARCHAR(255) NOT NULL REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		label VARCHAR(255) NOT NULL,
		confidence FLOAT NOT NULL,
		bounding_box JSONB NOT NULL,
		timestamp FLOAT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Text extractions (OCR)
	CREATE TABLE IF NOT EXISTS videoagent.text_extractions (
		text_id VARCHAR(255) PRIMARY KEY,
		frame_id VARCHAR(255) REFERENCES videoagent.frames(frame_id) ON DELETE CASCADE,
		job_id VARCHAR(255) NOT NULL REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		text TEXT NOT NULL,
		confidence FLOAT NOT NULL,
		bounding_box JSONB NOT NULL,
		language VARCHAR(50),
		timestamp FLOAT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Audio analysis
	CREATE TABLE IF NOT EXISTS videoagent.audio_analysis (
		job_id VARCHAR(255) PRIMARY KEY REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		transcription TEXT,
		language VARCHAR(50),
		confidence FLOAT,
		sentiment VARCHAR(50),
		topics JSONB,
		keywords JSONB,
		audio_file_path TEXT,
		model_used VARCHAR(255),
		processing_time FLOAT,
		speakers JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Scene detections
	CREATE TABLE IF NOT EXISTS videoagent.scenes (
		scene_id VARCHAR(255) PRIMARY KEY,
		job_id VARCHAR(255) NOT NULL REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		start_time FLOAT NOT NULL,
		end_time FLOAT NOT NULL,
		start_frame INT NOT NULL,
		end_frame INT NOT NULL,
		description TEXT,
		key_frame_id VARCHAR(255) REFERENCES videoagent.frames(frame_id),
		scene_type VARCHAR(100),
		confidence FLOAT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Content classification
	CREATE TABLE IF NOT EXISTS videoagent.classifications (
		job_id VARCHAR(255) PRIMARY KEY REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		primary_category VARCHAR(255),
		categories JSONB NOT NULL,
		tags JSONB,
		content_rating VARCHAR(50),
		is_nsfw BOOLEAN,
		confidence FLOAT,
		model_used VARCHAR(255),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Model usage tracking (for learning and cost optimization)
	CREATE TABLE IF NOT EXISTS videoagent.model_usage (
		id SERIAL PRIMARY KEY,
		job_id VARCHAR(255) REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		task_type VARCHAR(100) NOT NULL,
		model_id VARCHAR(255) NOT NULL,
		model_provider VARCHAR(100) NOT NULL,
		complexity FLOAT NOT NULL,
		cost FLOAT NOT NULL,
		duration FLOAT NOT NULL,
		success BOOLEAN NOT NULL,
		timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Processing summaries
	CREATE TABLE IF NOT EXISTS videoagent.processing_results (
		job_id VARCHAR(255) PRIMARY KEY REFERENCES videoagent.jobs(job_id) ON DELETE CASCADE,
		summary TEXT,
		total_frames INT,
		total_objects INT,
		total_scenes INT,
		processing_time FLOAT,
		total_cost FLOAT,
		result_data JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Google Drive OAuth tokens
	CREATE TABLE IF NOT EXISTS videoagent.gdrive_tokens (
		user_id VARCHAR(255) PRIMARY KEY,
		access_token TEXT NOT NULL,
		refresh_token TEXT,
		expiry_date TIMESTAMP,
		token_type VARCHAR(50),
		scope TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`

	// Execute table creation
	if _, err := sm.db.Exec(tableSchema); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Step 2: Create indexes separately (PostgreSQL-native approach)
	// Using IF NOT EXISTS to make index creation idempotent
	indexStatements := []string{
		// Jobs table indexes
		`CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON videoagent.jobs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON videoagent.jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON videoagent.jobs(created_at)`,

		// Frames table indexes
		`CREATE INDEX IF NOT EXISTS idx_frames_job_id ON videoagent.frames(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_frames_timestamp ON videoagent.frames(timestamp)`,

		// Objects table indexes
		`CREATE INDEX IF NOT EXISTS idx_objects_job_id ON videoagent.objects(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_frame_id ON videoagent.objects(frame_id)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_label ON videoagent.objects(label)`,

		// Text extractions table indexes
		`CREATE INDEX IF NOT EXISTS idx_text_job_id ON videoagent.text_extractions(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_text_frame_id ON videoagent.text_extractions(frame_id)`,

		// Scenes table indexes
		`CREATE INDEX IF NOT EXISTS idx_scenes_job_id ON videoagent.scenes(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_scenes_start_time ON videoagent.scenes(start_time)`,
		`CREATE INDEX IF NOT EXISTS idx_scenes_end_time ON videoagent.scenes(end_time)`,

		// Model usage table indexes
		`CREATE INDEX IF NOT EXISTS idx_usage_job_id ON videoagent.model_usage(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_model ON videoagent.model_usage(model_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_task ON videoagent.model_usage(task_type)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_timestamp ON videoagent.model_usage(timestamp)`,
	}

	// Execute index creation statements
	for _, stmt := range indexStatements {
		if _, err := sm.db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to create index: %w (statement: %s)", err, stmt)
		}
	}

	return nil
}

// initQdrantCollection creates Qdrant collection for frame embeddings
func (sm *StorageManager) initQdrantCollection() error {
	// DISABLED: Qdrant operations now handled by similarity.QdrantManager
	// This method is a no-op stub to maintain interface compatibility
	return nil

	/* COMMENTED OUT - Qdrant client v1.7.0 API incompatible
	ctx := context.Background()

	// Check if collection exists
	exists, err := sm.qdrantClient.CollectionExists(ctx, sm.collectionName)
	if err != nil {
		return fmt.Errorf("failed to check collection existence: %w", err)
	}

	if exists {
		return nil
	}

	// Create collection with vector configuration
	// Using 1024 dimensions for VoyageAI voyage-3 embeddings
	vectorParams := &qdrant.VectorParams{
		Size:     1024, // VoyageAI voyage-3 dimensions
		Distance: qdrant.Distance_Cosine,
	}

	err = sm.qdrantClient.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: sm.collectionName,
		VectorsConfig: &qdrant.VectorsConfig{
			Config: &qdrant.VectorsConfig_Params{
				Params: vectorParams,
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to create Qdrant collection: %w", err)
	}

	return nil
	*/
}

// StoreJob stores job information
func (sm *StorageManager) StoreJob(ctx context.Context, job *models.JobPayload) error {
	query := `
		INSERT INTO videoagent.jobs (job_id, user_id, session_id, video_url, source_type, status, options, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (job_id) DO UPDATE SET
			status = EXCLUDED.status,
			metadata = EXCLUDED.metadata
	`

	optionsJSON, err := json.Marshal(job.Options)
	if err != nil {
		return fmt.Errorf("failed to marshal options: %w", err)
	}

	metadataJSON, err := json.Marshal(job.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = sm.db.ExecContext(ctx, query,
		job.JobID,
		job.UserID,
		job.SessionID,
		job.VideoURL,
		job.SourceType,
		"pending",
		optionsJSON,
		metadataJSON,
		job.EnqueuedAt,
	)

	return err
}

// UpdateJobStatus updates job status
func (sm *StorageManager) UpdateJobStatus(ctx context.Context, jobID, status string, errorMsg string) error {
	query := `
		UPDATE videoagent.jobs
		SET status = $2, error = $3, completed_at = CASE WHEN $2 IN ('completed', 'failed') THEN CURRENT_TIMESTAMP ELSE completed_at END
		WHERE job_id = $1
	`

	_, err := sm.db.ExecContext(ctx, query, jobID, status, errorMsg)
	return err
}

// StoreVideoMetadata stores video metadata
func (sm *StorageManager) StoreVideoMetadata(ctx context.Context, jobID string, metadata *models.VideoMetadata) error {
	query := `
		INSERT INTO videoagent.video_metadata (job_id, duration, width, height, frame_rate, codec, bitrate, size, format, audio_codec, audio_tracks, has_subtitles, quality)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (job_id) DO UPDATE SET
			duration = EXCLUDED.duration,
			width = EXCLUDED.width,
			height = EXCLUDED.height,
			frame_rate = EXCLUDED.frame_rate,
			codec = EXCLUDED.codec,
			bitrate = EXCLUDED.bitrate,
			size = EXCLUDED.size,
			format = EXCLUDED.format,
			audio_codec = EXCLUDED.audio_codec,
			audio_tracks = EXCLUDED.audio_tracks,
			has_subtitles = EXCLUDED.has_subtitles,
			quality = EXCLUDED.quality
	`

	_, err := sm.db.ExecContext(ctx, query,
		jobID,
		metadata.Duration,
		metadata.Width,
		metadata.Height,
		metadata.FrameRate,
		metadata.Codec,
		metadata.Bitrate,
		metadata.Size,
		metadata.Format,
		metadata.AudioCodec,
		metadata.AudioTracks,
		metadata.HasSubtitles,
		metadata.Quality,
	)

	return err
}

// StoreFrame stores frame analysis with vector embedding in Qdrant
func (sm *StorageManager) StoreFrame(ctx context.Context, frame *models.FrameAnalysis, jobID string) error {
	tx, err := sm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Store frame in PostgreSQL
	query := `
		INSERT INTO videoagent.frames (frame_id, job_id, timestamp, frame_number, file_path, description, confidence, model_used, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	metadataJSON, _ := json.Marshal(frame.Metadata)

	_, err = tx.ExecContext(ctx, query,
		frame.FrameID,
		jobID,
		frame.Timestamp,
		frame.FrameNumber,
		frame.FilePath,
		frame.Description,
		frame.Confidence,
		frame.ModelUsed,
		metadataJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to store frame: %w", err)
	}

	// Store frame embedding in Qdrant if available
	// DISABLED: Qdrant operations now handled by similarity.QdrantManager
	// Vector embeddings are stored via similarity module, not this storage manager
	if len(frame.Embedding) > 0 {
		// Log that embeddings exist but are handled elsewhere
		_ = frame.Embedding // Acknowledge we saw the embeddings
	}

	/* COMMENTED OUT - Qdrant operations disabled
	if len(frame.Embedding) > 0 {
		point := &qdrant.PointStruct{
			Id: &qdrant.PointId{
				PointIdOptions: &qdrant.PointId_Uuid{
					Uuid: frame.FrameID,
				},
			},
			Vectors: &qdrant.Vectors{
				VectorsOptions: &qdrant.Vectors_Vector{
					Vector: &qdrant.Vector{
						Data: frame.Embedding,
					},
				},
			},
			Payload: map[string]*qdrant.Value{
				"job_id":       {Kind: &qdrant.Value_StringValue{StringValue: jobID}},
				"timestamp":    {Kind: &qdrant.Value_DoubleValue{DoubleValue: frame.Timestamp}},
				"frame_number": {Kind: &qdrant.Value_IntegerValue{IntegerValue: int64(frame.FrameNumber)}},
				"description":  {Kind: &qdrant.Value_StringValue{StringValue: frame.Description}},
			},
		}

		_, err = sm.qdrantClient.Upsert(ctx, &qdrant.UpsertPoints{
			CollectionName: sm.collectionName,
			Points:         []*qdrant.PointStruct{point},
		})

		if err != nil {
			return fmt.Errorf("failed to store frame embedding: %w", err)
		}
	}
	*/

	return tx.Commit()
}

// StoreObjects stores detected objects
func (sm *StorageManager) StoreObjects(ctx context.Context, jobID, frameID string, objects []models.ObjectDetection) error {
	if len(objects) == 0 {
		return nil
	}

	query := `
		INSERT INTO videoagent.objects (object_id, frame_id, job_id, label, confidence, bounding_box, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	for _, obj := range objects {
		bboxJSON, _ := json.Marshal(obj.BoundingBox)

		_, err := sm.db.ExecContext(ctx, query,
			obj.ObjectID,
			frameID,
			jobID,
			obj.Label,
			obj.Confidence,
			bboxJSON,
			obj.Timestamp,
		)

		if err != nil {
			return fmt.Errorf("failed to store object: %w", err)
		}
	}

	return nil
}

// StoreAudioAnalysis stores audio transcription and analysis
func (sm *StorageManager) StoreAudioAnalysis(ctx context.Context, jobID string, analysis *models.AudioAnalysis) error {
	query := `
		INSERT INTO videoagent.audio_analysis (job_id, transcription, language, confidence, sentiment, topics, keywords, audio_file_path, model_used, processing_time, speakers)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (job_id) DO UPDATE SET
			transcription = EXCLUDED.transcription,
			language = EXCLUDED.language,
			confidence = EXCLUDED.confidence,
			sentiment = EXCLUDED.sentiment,
			topics = EXCLUDED.topics,
			keywords = EXCLUDED.keywords,
			audio_file_path = EXCLUDED.audio_file_path,
			model_used = EXCLUDED.model_used,
			processing_time = EXCLUDED.processing_time,
			speakers = EXCLUDED.speakers
	`

	topicsJSON, _ := json.Marshal(analysis.Topics)
	keywordsJSON, _ := json.Marshal(analysis.Keywords)
	speakersJSON, _ := json.Marshal(analysis.Speakers)

	_, err := sm.db.ExecContext(ctx, query,
		jobID,
		analysis.Transcription,
		analysis.Language,
		analysis.Confidence,
		analysis.Sentiment,
		topicsJSON,
		keywordsJSON,
		analysis.AudioFilePath,
		analysis.ModelUsed,
		analysis.ProcessingTime,
		speakersJSON,
	)

	return err
}

// StoreModelUsage tracks model usage for learning
func (sm *StorageManager) StoreModelUsage(ctx context.Context, jobID string, usage models.ModelUsageRecord) error {
	query := `
		INSERT INTO videoagent.model_usage (job_id, task_type, model_id, model_provider, complexity, cost, duration, success)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := sm.db.ExecContext(ctx, query,
		jobID,
		usage.TaskType,
		usage.ModelID,
		usage.ModelProvider,
		usage.Complexity,
		usage.Cost,
		usage.Duration,
		usage.Success,
	)

	return err
}

// StoreProcessingResult stores final processing result
func (sm *StorageManager) StoreProcessingResult(ctx context.Context, result *models.ProcessingResult) error {
	query := `
		INSERT INTO videoagent.processing_results (job_id, summary, total_frames, total_objects, total_scenes, processing_time, total_cost, result_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (job_id) DO UPDATE SET
			summary = EXCLUDED.summary,
			total_frames = EXCLUDED.total_frames,
			total_objects = EXCLUDED.total_objects,
			total_scenes = EXCLUDED.total_scenes,
			processing_time = EXCLUDED.processing_time,
			total_cost = EXCLUDED.total_cost,
			result_data = EXCLUDED.result_data
	`

	resultJSON, _ := json.Marshal(result)

	totalCost := 0.0
	for _, usage := range result.ModelUsage {
		totalCost += usage.Cost
	}

	_, err := sm.db.ExecContext(ctx, query,
		result.JobID,
		result.Summary,
		len(result.Frames),
		len(result.Objects),
		len(result.Scenes),
		result.ProcessingTime,
		totalCost,
		resultJSON,
	)

	return err
}

// SearchSimilarFrames searches for similar frames using vector similarity
// DISABLED: Vector search operations are now handled by similarity.SearchAPI
// Use similarity.SearchAPI.SearchByText() or SearchByVideo() instead
func (sm *StorageManager) SearchSimilarFrames(ctx context.Context, embedding []float32, limit int) ([]*models.FrameAnalysis, error) {
	return nil, fmt.Errorf("SearchSimilarFrames is disabled - use similarity.SearchAPI for vector search operations")

	/* COMMENTED OUT - Qdrant operations disabled, use similarity module instead
	// Search Qdrant for similar vectors
	searchResult, err := sm.qdrantClient.Search(ctx, &qdrant.SearchPoints{
		CollectionName: sm.collectionName,
		Vector:         embedding,
		Limit:          uint64(limit),
		WithPayload:    &qdrant.WithPayloadSelector{SelectorOptions: &qdrant.WithPayloadSelector_Enable{Enable: true}},
	})

	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Fetch full frame details from PostgreSQL
	var frames []*models.FrameAnalysis
	for _, point := range searchResult {
		frameID := point.Id.GetUuid()

		var frame models.FrameAnalysis
		query := `
			SELECT frame_id, job_id, timestamp, frame_number, file_path, description, confidence, model_used
			FROM videoagent.frames
			WHERE frame_id = $1
		`

		var jobID string
		err := sm.db.QueryRowContext(ctx, query, frameID).Scan(
			&frame.FrameID,
			&jobID,
			&frame.Timestamp,
			&frame.FrameNumber,
			&frame.FilePath,
			&frame.Description,
			&frame.Confidence,
			&frame.ModelUsed,
		)

		if err != nil {
			continue // Skip if not found
		}

		frames = append(frames, &frame)
	}

	return frames, nil
	*/
}

// StoreClassification stores content classification results in both PostgreSQL and GraphRAG
func (sm *StorageManager) StoreClassification(ctx context.Context, jobID string, classification *models.ContentClassification) error {
	// Step 1: Store in PostgreSQL (structured queryable storage)
	query := `
		INSERT INTO videoagent.classifications (
			job_id, primary_category, categories, tags, content_rating, is_nsfw, confidence, model_used
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (job_id) DO UPDATE SET
			primary_category = EXCLUDED.primary_category,
			categories = EXCLUDED.categories,
			tags = EXCLUDED.tags,
			content_rating = EXCLUDED.content_rating,
			is_nsfw = EXCLUDED.is_nsfw,
			confidence = EXCLUDED.confidence,
			model_used = EXCLUDED.model_used
	`

	categoriesJSON, err := json.Marshal(classification.Categories)
	if err != nil {
		return fmt.Errorf("failed to marshal categories: %w", err)
	}

	tagsJSON, err := json.Marshal(classification.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	_, err = sm.db.ExecContext(ctx, query,
		jobID,
		classification.PrimaryCategory,
		categoriesJSON,
		tagsJSON,
		classification.ContentRating,
		classification.IsNSFW,
		classification.Confidence,
		classification.ModelUsed,
	)

	if err != nil {
		return fmt.Errorf("failed to store classification in PostgreSQL: %w", err)
	}

	// Step 2: Store in GraphRAG for semantic search via MageAgent
	// This will be implemented by calling MageAgent's brain_store_entity endpoint
	// For now, we log that this should be added
	// TODO: Call MageAgent Brain API to store classification as entity
	// Example: POST /api/brain/store-entity with classification data

	return nil
}

// Close closes all connections
func (sm *StorageManager) Close() error {
	if sm.db != nil {
		sm.db.Close()
	}
	// Qdrant client doesn't need explicit close
	return nil
}
