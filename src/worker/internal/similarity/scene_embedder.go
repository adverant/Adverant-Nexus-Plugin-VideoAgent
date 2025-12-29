package similarity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"time"
)

// SceneEmbedding represents a scene-level embedding
type SceneEmbedding struct {
	SceneID       string                 `json:"sceneId"`
	VideoID       string                 `json:"videoId"`
	StartFrame    int                    `json:"startFrame"`
	EndFrame      int                    `json:"endFrame"`
	Duration      float64                `json:"duration"`       // Seconds
	Embedding     []float64              `json:"embedding"`      // 1024-dimensional vector (VoyageAI voyage-3)
	SceneType     string                 `json:"sceneType"`      // interior, exterior, action, etc.
	ShotCount     int                    `json:"shotCount"`      // Number of shots
	Shots         []ShotInfo             `json:"shots"`          // Individual shots
	Semantics     SceneSemantics         `json:"semantics"`      // Semantic content
	Visual        SceneVisual            `json:"visual"`         // Visual characteristics
	Audio         SceneAudio             `json:"audio"`          // Audio characteristics
	Motion        SceneMotion            `json:"motion"`         // Motion characteristics
	Timestamp     time.Time              `json:"timestamp"`
	Hash          string                 `json:"hash"`
}

// ShotInfo represents information about a shot within a scene
type ShotInfo struct {
	ShotNum      int       `json:"shotNum"`
	StartFrame   int       `json:"startFrame"`
	EndFrame     int       `json:"endFrame"`
	Duration     float64   `json:"duration"`
	ShotSize     string    `json:"shotSize"`     // close_up, medium, wide, etc.
	ShotAngle    string    `json:"shotAngle"`    // eye_level, high_angle, etc.
	CameraMove   string    `json:"cameraMove"`   // static, pan, tilt, etc.
	Embedding    []float64 `json:"embedding"`    // Shot-level embedding
}

// SceneSemantics represents semantic content of a scene
type SceneSemantics struct {
	Objects       []string               `json:"objects"`        // Detected objects
	Actions       []string               `json:"actions"`        // Detected actions
	People        []string               `json:"people"`         // People descriptions
	Location      string                 `json:"location"`       // Location description
	Time          string                 `json:"time"`           // Time of day
	Weather       string                 `json:"weather"`        // Weather conditions
	Mood          string                 `json:"mood"`           // Emotional mood
	Narrative     string                 `json:"narrative"`      // Brief narrative summary
	Tags          []string               `json:"tags"`           // Semantic tags
	Attributes    map[string]interface{} `json:"attributes"`
}

// SceneVisual represents visual characteristics
type SceneVisual struct {
	ColorGrading    string    `json:"colorGrading"`    // Color grading style
	Lighting        string    `json:"lighting"`        // Lighting setup
	Brightness      float64   `json:"brightness"`      // Average brightness
	Contrast        float64   `json:"contrast"`        // Average contrast
	Saturation      float64   `json:"saturation"`      // Color saturation
	DominantColors  []string  `json:"dominantColors"`  // Dominant colors
	Composition     string    `json:"composition"`     // Overall composition style
	Depth           string    `json:"depth"`           // Depth perception
}

// SceneAudio represents audio characteristics
type SceneAudio struct {
	HasDialogue   bool     `json:"hasDialogue"`
	HasMusic      bool     `json:"hasMusic"`
	HasSFX        bool     `json:"hasSFX"`         // Sound effects
	Volume        float64  `json:"volume"`         // Average volume
	Tempo         string   `json:"tempo"`          // Slow, medium, fast
	MusicGenre    string   `json:"musicGenre"`     // If music present
	AudioMood     string   `json:"audioMood"`      // Audio emotional tone
}

// SceneMotion represents motion characteristics
type SceneMotion struct {
	AvgMotion      float64  `json:"avgMotion"`      // 0-1, average motion
	MaxMotion      float64  `json:"maxMotion"`      // Peak motion
	MotionPattern  string   `json:"motionPattern"`  // static, flowing, chaotic, etc.
	CameraMotion   string   `json:"cameraMotion"`   // Dominant camera movement
	SubjectMotion  string   `json:"subjectMotion"`  // Subject movement pattern
}

// SceneEmbedder generates scene-level embeddings
type SceneEmbedder struct {
	videoEmbedder    *VideoEmbedder
	minSceneLength   int     // Minimum frames for a scene
	maxSceneLength   int     // Maximum frames for a scene
	sceneThreshold   float64 // Threshold for scene boundary detection
}

// NewSceneEmbedder creates a new scene embedder
func NewSceneEmbedder(videoEmbedder *VideoEmbedder) *SceneEmbedder {
	return &SceneEmbedder{
		videoEmbedder:  videoEmbedder,
		minSceneLength: 30,   // 1 second at 30fps
		maxSceneLength: 900,  // 30 seconds at 30fps
		sceneThreshold: 0.7,  // Similarity threshold for scene boundary
	}
}

// GenerateSceneEmbeddings generates embeddings for all scenes in a video
func (se *SceneEmbedder) GenerateSceneEmbeddings(ctx context.Context, videoID string, frames []string, videoEmbedding *VideoEmbedding) ([]SceneEmbedding, error) {
	startTime := time.Now()

	// Detect scene boundaries
	sceneBoundaries := se.detectSceneBoundaries(videoEmbedding.FrameEmbeddings)
	log.Printf("Detected %d scenes in video %s", len(sceneBoundaries)-1, videoID)

	// Generate embedding for each scene
	sceneEmbeddings := make([]SceneEmbedding, 0, len(sceneBoundaries)-1)

	for i := 0; i < len(sceneBoundaries)-1; i++ {
		startFrame := sceneBoundaries[i]
		endFrame := sceneBoundaries[i+1]

		sceneEmbedding, err := se.generateSceneEmbedding(ctx, videoID, i+1, startFrame, endFrame, frames, videoEmbedding)
		if err != nil {
			log.Printf("Warning: Scene %d embedding failed: %v", i+1, err)
			continue
		}

		sceneEmbeddings = append(sceneEmbeddings, *sceneEmbedding)

		log.Printf("Generated scene %d embedding: frames %d-%d (%.2fs)",
			i+1, startFrame, endFrame, sceneEmbedding.Duration)
	}

	elapsed := time.Since(startTime).Seconds()
	log.Printf("Generated %d scene embeddings in %.2fs", len(sceneEmbeddings), elapsed)

	return sceneEmbeddings, nil
}

// detectSceneBoundaries detects scene boundaries using frame embeddings
func (se *SceneEmbedder) detectSceneBoundaries(frameEmbeddings []FrameEmbedding) []int {
	if len(frameEmbeddings) == 0 {
		return []int{0}
	}

	boundaries := []int{0} // Start with frame 0

	for i := 1; i < len(frameEmbeddings); i++ {
		// Compute similarity with previous frame
		similarity := se.computeCosineSimilarity(
			frameEmbeddings[i-1].Embedding,
			frameEmbeddings[i].Embedding,
		)

		// If similarity drops below threshold, mark boundary
		if similarity < se.sceneThreshold {
			// Check minimum scene length
			if frameEmbeddings[i].FrameNum-boundaries[len(boundaries)-1] >= se.minSceneLength {
				boundaries = append(boundaries, frameEmbeddings[i].FrameNum)
				log.Printf("Scene boundary detected at frame %d (similarity: %.2f)",
					frameEmbeddings[i].FrameNum, similarity)
			}
		}

		// Check maximum scene length
		if frameEmbeddings[i].FrameNum-boundaries[len(boundaries)-1] >= se.maxSceneLength {
			boundaries = append(boundaries, frameEmbeddings[i].FrameNum)
			log.Printf("Scene boundary forced at frame %d (max length reached)",
				frameEmbeddings[i].FrameNum)
		}
	}

	// Add final boundary
	if len(frameEmbeddings) > 0 {
		lastFrame := frameEmbeddings[len(frameEmbeddings)-1].FrameNum
		if lastFrame != boundaries[len(boundaries)-1] {
			boundaries = append(boundaries, lastFrame)
		}
	}

	return boundaries
}

// generateSceneEmbedding generates embedding for a single scene
func (se *SceneEmbedder) generateSceneEmbedding(ctx context.Context, videoID string, sceneNum, startFrame, endFrame int, frames []string, videoEmbedding *VideoEmbedding) (*SceneEmbedding, error) {
	// Find frame embeddings within scene
	sceneFrames := se.getFramesInRange(videoEmbedding.FrameEmbeddings, startFrame, endFrame)

	if len(sceneFrames) == 0 {
		return nil, fmt.Errorf("no frames found for scene %d", sceneNum)
	}

	// Aggregate frame embeddings
	sceneEmbeddingVec := se.aggregateSceneFrames(sceneFrames)

	// Detect shots within scene
	shots := se.detectShots(sceneFrames)

	// Extract semantic content
	semantics := se.extractSemantics(sceneFrames)

	// Extract visual characteristics
	visual := se.extractVisual(sceneFrames)

	// Extract audio characteristics (placeholder)
	audio := se.extractAudio(sceneFrames)

	// Extract motion characteristics
	motion := se.extractMotion(sceneFrames)

	// Compute hash
	hash := se.computeSceneHash(sceneEmbeddingVec)

	sceneID := fmt.Sprintf("%s_scene_%d", videoID, sceneNum)
	duration := float64(endFrame-startFrame) / 30.0 // Assume 30fps

	scene := &SceneEmbedding{
		SceneID:    sceneID,
		VideoID:    videoID,
		StartFrame: startFrame,
		EndFrame:   endFrame,
		Duration:   duration,
		Embedding:  sceneEmbeddingVec,
		SceneType:  semantics.Location, // Simplified
		ShotCount:  len(shots),
		Shots:      shots,
		Semantics:  semantics,
		Visual:     visual,
		Audio:      audio,
		Motion:     motion,
		Timestamp:  time.Now(),
		Hash:       hash,
	}

	return scene, nil
}

// getFramesInRange gets frame embeddings within a range
func (se *SceneEmbedder) getFramesInRange(frameEmbeddings []FrameEmbedding, startFrame, endFrame int) []FrameEmbedding {
	result := make([]FrameEmbedding, 0)

	for _, frame := range frameEmbeddings {
		if frame.FrameNum >= startFrame && frame.FrameNum <= endFrame {
			result = append(result, frame)
		}
	}

	return result
}

// aggregateSceneFrames aggregates frame embeddings for scene
func (se *SceneEmbedder) aggregateSceneFrames(frames []FrameEmbedding) []float64 {
	if len(frames) == 0 {
		return make([]float64, EmbeddingDimension)
	}

	// Mean aggregation
	aggregated := make([]float64, EmbeddingDimension)

	for _, frame := range frames {
		for i, val := range frame.Embedding {
			aggregated[i] += val
		}
	}

	n := float64(len(frames))
	for i := range aggregated {
		aggregated[i] /= n
	}

	return aggregated
}

// detectShots detects shots within a scene
func (se *SceneEmbedder) detectShots(sceneFrames []FrameEmbedding) []ShotInfo {
	if len(sceneFrames) == 0 {
		return []ShotInfo{}
	}

	shots := make([]ShotInfo, 0)
	shotStart := 0
	shotNum := 1

	// Simple shot detection based on embedding similarity
	shotThreshold := 0.85 // Higher threshold than scene detection

	for i := 1; i < len(sceneFrames); i++ {
		similarity := se.computeCosineSimilarity(
			sceneFrames[i-1].Embedding,
			sceneFrames[i].Embedding,
		)

		if similarity < shotThreshold && i-shotStart >= 5 { // Min 5 frames per shot
			// End current shot
			shot := se.createShot(shotNum, sceneFrames[shotStart:i])
			shots = append(shots, shot)

			shotStart = i
			shotNum++
		}
	}

	// Add final shot
	if shotStart < len(sceneFrames) {
		shot := se.createShot(shotNum, sceneFrames[shotStart:])
		shots = append(shots, shot)
	}

	return shots
}

// createShot creates a shot from frame embeddings
func (se *SceneEmbedder) createShot(shotNum int, frames []FrameEmbedding) ShotInfo {
	if len(frames) == 0 {
		return ShotInfo{ShotNum: shotNum}
	}

	startFrame := frames[0].FrameNum
	endFrame := frames[len(frames)-1].FrameNum
	duration := float64(endFrame-startFrame) / 30.0

	// Aggregate shot embedding
	embedding := se.aggregateSceneFrames(frames)

	// Extract shot characteristics from features
	shotSize := "medium" // Default
	shotAngle := "eye_level"
	cameraMove := "static"

	// Could extract from frame features if available

	return ShotInfo{
		ShotNum:    shotNum,
		StartFrame: startFrame,
		EndFrame:   endFrame,
		Duration:   duration,
		ShotSize:   shotSize,
		ShotAngle:  shotAngle,
		CameraMove: cameraMove,
		Embedding:  embedding,
	}
}

// extractSemantics extracts semantic content from frames
func (se *SceneEmbedder) extractSemantics(frames []FrameEmbedding) SceneSemantics {
	objectMap := make(map[string]int)
	sceneMap := make(map[string]int)

	for _, frame := range frames {
		for _, obj := range frame.Features.Objects {
			objectMap[obj]++
		}
		sceneMap[frame.Features.Scene]++
	}

	// Most common objects
	objects := se.videoEmbedder.topN(objectMap, 10)

	// Dominant scene type
	sceneTypes := se.videoEmbedder.topN(sceneMap, 1)
	location := "unknown"
	if len(sceneTypes) > 0 {
		location = sceneTypes[0]
	}

	return SceneSemantics{
		Objects:    objects,
		Actions:    []string{}, // Could be extracted from MageAgent
		People:     []string{}, // Could be extracted from tracking
		Location:   location,
		Time:       "unknown",
		Weather:    "unknown",
		Mood:       "neutral",
		Narrative:  "",
		Tags:       objects,
		Attributes: make(map[string]interface{}),
	}
}

// extractVisual extracts visual characteristics
func (se *SceneEmbedder) extractVisual(frames []FrameEmbedding) SceneVisual {
	if len(frames) == 0 {
		return SceneVisual{}
	}

	avgBrightness := 0.0
	avgContrast := 0.0
	avgSaturation := 0.5 // Placeholder

	colorMap := make(map[string]int)

	for _, frame := range frames {
		avgBrightness += frame.Features.Brightness
		avgContrast += frame.Features.Contrast

		for _, color := range frame.Features.DominantColors {
			colorMap[color]++
		}
	}

	n := float64(len(frames))
	avgBrightness /= n
	avgContrast /= n

	dominantColors := se.videoEmbedder.topN(colorMap, 5)

	return SceneVisual{
		ColorGrading:   "natural",
		Lighting:       "natural",
		Brightness:     avgBrightness,
		Contrast:       avgContrast,
		Saturation:     avgSaturation,
		DominantColors: dominantColors,
		Composition:    "balanced",
		Depth:          "medium",
	}
}

// extractAudio extracts audio characteristics (placeholder)
func (se *SceneEmbedder) extractAudio(frames []FrameEmbedding) SceneAudio {
	// Placeholder - would require actual audio analysis
	return SceneAudio{
		HasDialogue: false,
		HasMusic:    false,
		HasSFX:      false,
		Volume:      0.5,
		Tempo:       "medium",
		MusicGenre:  "unknown",
		AudioMood:   "neutral",
	}
}

// extractMotion extracts motion characteristics
func (se *SceneEmbedder) extractMotion(frames []FrameEmbedding) SceneMotion {
	if len(frames) == 0 {
		return SceneMotion{}
	}

	avgMotion := 0.0
	maxMotion := 0.0

	for _, frame := range frames {
		avgMotion += frame.Features.Motion
		if frame.Features.Motion > maxMotion {
			maxMotion = frame.Features.Motion
		}
	}

	avgMotion /= float64(len(frames))

	motionPattern := "static"
	if avgMotion > 0.7 {
		motionPattern = "chaotic"
	} else if avgMotion > 0.4 {
		motionPattern = "flowing"
	} else if avgMotion > 0.1 {
		motionPattern = "moderate"
	}

	return SceneMotion{
		AvgMotion:     avgMotion,
		MaxMotion:     maxMotion,
		MotionPattern: motionPattern,
		CameraMotion:  "static",
		SubjectMotion: motionPattern,
	}
}

// computeCosineSimilarity computes cosine similarity between vectors
func (se *SceneEmbedder) computeCosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) {
		return 0.0
	}

	var dotProduct, norm1, norm2 float64
	for i := range vec1 {
		dotProduct += vec1[i] * vec2[i]
		norm1 += vec1[i] * vec1[i]
		norm2 += vec2[i] * vec2[i]
	}

	if norm1 == 0 || norm2 == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

// computeSceneHash computes content hash
func (se *SceneEmbedder) computeSceneHash(embedding []float64) string {
	data := make([]byte, 0, len(embedding)*8)
	for _, val := range embedding {
		bits := math.Float64bits(val)
		for i := 0; i < 8; i++ {
			data = append(data, byte(bits>>(i*8)))
		}
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
