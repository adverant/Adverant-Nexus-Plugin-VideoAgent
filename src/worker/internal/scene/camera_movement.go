package scene

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// CameraMovementType represents the type of camera movement
type CameraMovementType string

const (
	MovementStatic   CameraMovementType = "static"
	MovementPan      CameraMovementType = "pan"
	MovementTilt     CameraMovementType = "tilt"
	MovementZoom     CameraMovementType = "zoom"
	MovementDolly    CameraMovementType = "dolly"
	MovementTracking CameraMovementType = "tracking"
	MovementCrane    CameraMovementType = "crane"
	MovementHandheld CameraMovementType = "handheld"
)

// MovementSpeed represents the speed of camera movement
type MovementSpeed string

const (
	SpeedStill    MovementSpeed = "still"
	SpeedVerySlow MovementSpeed = "very_slow"
	SpeedSlow     MovementSpeed = "slow"
	SpeedMedium   MovementSpeed = "medium"
	SpeedFast     MovementSpeed = "fast"
	SpeedVeryFast MovementSpeed = "very_fast"
)

// MovementSmoothness represents the smoothness of camera movement
type MovementSmoothness string

const (
	SmoothnessVerySmooth MovementSmoothness = "very_smooth"
	SmoothnessSmooth     MovementSmoothness = "smooth"
	SmoothnessMedium     MovementSmoothness = "medium"
	SmoothnessJerky      MovementSmoothness = "jerky"
	SmoothnessVeryJerky  MovementSmoothness = "very_jerky"
)

// CameraMovement represents detected camera movement in a frame
type CameraMovement struct {
	PrimaryMovement   CameraMovementType     `json:"primaryMovement"`
	SecondaryMovement *CameraMovementType    `json:"secondaryMovement,omitempty"`
	Speed             MovementSpeed          `json:"speed"`
	Smoothness        MovementSmoothness     `json:"smoothness"`
	Direction         *MovementDirection     `json:"direction,omitempty"`
	Trajectory        []TrajectoryPoint      `json:"trajectory,omitempty"`
	Stabilization     StabilizationAnalysis  `json:"stabilization"`
	Shake             ShakeAnalysis          `json:"shake"`
	Confidence        float64                `json:"confidence"`
	Attributes        map[string]interface{} `json:"attributes"`
	Timestamp         time.Time              `json:"timestamp"`
}

// MovementDirection represents the direction of camera movement
type MovementDirection struct {
	Horizontal string  `json:"horizontal"` // left, right, none
	Vertical   string  `json:"vertical"`   // up, down, none
	Depth      string  `json:"depth"`      // in, out, none
	Angle      float64 `json:"angle"`      // degrees (0-360)
}

// TrajectoryPoint represents a point in the camera movement trajectory
type TrajectoryPoint struct {
	X         float64   `json:"x"`         // Normalized 0-1
	Y         float64   `json:"y"`         // Normalized 0-1
	Z         float64   `json:"z"`         // Depth estimate
	Timestamp time.Time `json:"timestamp"` // When this point occurred
}

// StabilizationAnalysis represents camera stabilization quality
type StabilizationAnalysis struct {
	IsStabilized bool    `json:"isStabilized"`
	Quality      string  `json:"quality"` // excellent, good, fair, poor, none
	Method       string  `json:"method"`  // tripod, gimbal, steadicam, optical, digital, handheld, none
	Confidence   float64 `json:"confidence"`
}

// ShakeAnalysis represents camera shake detection
type ShakeAnalysis struct {
	HasShake   bool    `json:"hasShake"`
	Intensity  string  `json:"intensity"` // none, minimal, moderate, significant, extreme
	Frequency  string  `json:"frequency"` // none, low, medium, high
	Intentional bool   `json:"intentional"` // True for deliberate shaky cam effect
	Confidence float64 `json:"confidence"`
}

// MovementHistory tracks movement across frames for pattern detection
type MovementHistory struct {
	Movements  []CameraMovement
	MaxHistory int
}

// CameraMovementDetector detects and analyzes camera movement
type CameraMovementDetector struct {
	mageAgent *clients.MageAgentClient
	history   *MovementHistory
}

// NewCameraMovementDetector creates a new camera movement detector
func NewCameraMovementDetector(mageAgent *clients.MageAgentClient) *CameraMovementDetector {
	return &CameraMovementDetector{
		mageAgent: mageAgent,
		history: &MovementHistory{
			Movements:  make([]CameraMovement, 0),
			MaxHistory: 30, // Keep last 30 frames
		},
	}
}

// DetectMovement detects camera movement in a frame
func (cmd *CameraMovementDetector) DetectMovement(ctx context.Context, frameData string) (*CameraMovement, error) {
	startTime := time.Now()

	// Prepare vision request with detailed prompt
	prompt := `Analyze this video frame and detect any camera movement with the following details:

1. PRIMARY MOVEMENT TYPE: Choose from static, pan, tilt, zoom, dolly, tracking, crane, handheld
   - static: No camera movement
   - pan: Horizontal rotation (left/right)
   - tilt: Vertical rotation (up/down)
   - zoom: Focal length change (in/out)
   - dolly: Physical camera movement forward/backward
   - tracking: Camera follows moving subject
   - crane: Vertical camera movement (up/down)
   - handheld: Unstabilized handheld camera

2. MOVEMENT SPEED: still, very_slow, slow, medium, fast, very_fast

3. SMOOTHNESS: very_smooth, smooth, medium, jerky, very_jerky

4. DIRECTION (if applicable):
   - Horizontal: left, right, none
   - Vertical: up, down, none
   - Depth: in, out, none
   - Angle: 0-360 degrees (0=right, 90=up, 180=left, 270=down)

5. STABILIZATION:
   - Is stabilized: yes/no
   - Quality: excellent, good, fair, poor, none
   - Method: tripod, gimbal, steadicam, optical, digital, handheld, none

6. SHAKE DETECTION:
   - Has shake: yes/no
   - Intensity: none, minimal, moderate, significant, extreme
   - Frequency: none, low, medium, high
   - Intentional (stylistic choice): yes/no

Respond with JSON format:
{
  "primaryMovement": "static|pan|tilt|zoom|dolly|tracking|crane|handheld",
  "secondaryMovement": "optional second movement type",
  "speed": "still|very_slow|slow|medium|fast|very_fast",
  "smoothness": "very_smooth|smooth|medium|jerky|very_jerky",
  "direction": {
    "horizontal": "left|right|none",
    "vertical": "up|down|none",
    "depth": "in|out|none",
    "angle": 0-360
  },
  "stabilization": {
    "isStabilized": true|false,
    "quality": "excellent|good|fair|poor|none",
    "method": "tripod|gimbal|steadicam|optical|digital|handheld|none"
  },
  "shake": {
    "hasShake": true|false,
    "intensity": "none|minimal|moderate|significant|extreme",
    "frequency": "none|low|medium|high",
    "intentional": true|false
  },
  "confidence": 0.0-1.0,
  "attributes": {
    "notes": "any additional observations"
  }
}`

	visionReq := models.MageAgentVisionRequest{
		Image:     frameData,
		Prompt:    prompt,
		MaxTokens: 1000,
	}

	visionResp, err := cmd.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Parse JSON response
	movement, err := cmd.parseMovementResponse(visionResp.Description)
	if err != nil {
		log.Printf("Failed to parse movement response, using fallback: %v", err)
		movement = cmd.fallbackMovementDetection(frameData)
	}

	movement.Timestamp = startTime

	// Add to history
	cmd.addToHistory(movement)

	// Enhance with trajectory analysis if we have history
	if len(cmd.history.Movements) > 1 {
		movement.Trajectory = cmd.computeTrajectory()
	}

	log.Printf("Detected camera movement: %s (speed: %s, smoothness: %s, confidence: %.2f)",
		movement.PrimaryMovement, movement.Speed, movement.Smoothness, movement.Confidence)

	return movement, nil
}

// DetectMovementBatch detects movement in multiple frames concurrently
func (cmd *CameraMovementDetector) DetectMovementBatch(ctx context.Context, frames []string) ([]*CameraMovement, error) {
	results := make([]*CameraMovement, len(frames))
	errChan := make(chan error, len(frames))

	for i, frame := range frames {
		go func(idx int, frameData string) {
			movement, err := cmd.DetectMovement(ctx, frameData)
			if err != nil {
				errChan <- fmt.Errorf("frame %d: %w", idx, err)
				return
			}
			results[idx] = movement
			errChan <- nil
		}(i, frame)
	}

	// Wait for all to complete
	for i := 0; i < len(frames); i++ {
		if err := <-errChan; err != nil {
			return nil, err
		}
	}

	return results, nil
}

// parseMovementResponse parses the AI response into CameraMovement
func (cmd *CameraMovementDetector) parseMovementResponse(response string) (*CameraMovement, error) {
	// Try to extract JSON from markdown code blocks
	jsonStr := extractJSON(response)

	var rawMovement struct {
		PrimaryMovement   string                 `json:"primaryMovement"`
		SecondaryMovement string                 `json:"secondaryMovement"`
		Speed             string                 `json:"speed"`
		Smoothness        string                 `json:"smoothness"`
		Direction         *MovementDirection     `json:"direction"`
		Stabilization     StabilizationAnalysis  `json:"stabilization"`
		Shake             ShakeAnalysis          `json:"shake"`
		Confidence        float64                `json:"confidence"`
		Attributes        map[string]interface{} `json:"attributes"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawMovement); err != nil {
		return nil, fmt.Errorf("failed to unmarshal movement JSON: %w", err)
	}

	movement := &CameraMovement{
		PrimaryMovement: CameraMovementType(rawMovement.PrimaryMovement),
		Speed:           MovementSpeed(rawMovement.Speed),
		Smoothness:      MovementSmoothness(rawMovement.Smoothness),
		Direction:       rawMovement.Direction,
		Stabilization:   rawMovement.Stabilization,
		Shake:           rawMovement.Shake,
		Confidence:      rawMovement.Confidence,
		Attributes:      rawMovement.Attributes,
	}

	if rawMovement.SecondaryMovement != "" {
		secondaryType := CameraMovementType(rawMovement.SecondaryMovement)
		movement.SecondaryMovement = &secondaryType
	}

	return movement, nil
}

// fallbackMovementDetection provides basic movement detection when AI parsing fails
func (cmd *CameraMovementDetector) fallbackMovementDetection(frameData string) *CameraMovement {
	// Default to static with low confidence
	movement := &CameraMovement{
		PrimaryMovement: MovementStatic,
		Speed:           SpeedStill,
		Smoothness:      SmoothnessSmooth,
		Stabilization: StabilizationAnalysis{
			IsStabilized: true,
			Quality:      "unknown",
			Method:       "none",
			Confidence:   0.3,
		},
		Shake: ShakeAnalysis{
			HasShake:   false,
			Intensity:  "none",
			Frequency:  "none",
			Intentional: false,
			Confidence: 0.3,
		},
		Confidence: 0.3,
		Attributes: map[string]interface{}{
			"fallback": true,
			"reason":   "AI parsing failed",
		},
	}

	return movement
}

// addToHistory adds movement to history and maintains max size
func (cmd *CameraMovementDetector) addToHistory(movement *CameraMovement) {
	cmd.history.Movements = append(cmd.history.Movements, *movement)

	// Trim to max history
	if len(cmd.history.Movements) > cmd.history.MaxHistory {
		cmd.history.Movements = cmd.history.Movements[1:]
	}
}

// computeTrajectory computes camera movement trajectory from history
func (cmd *CameraMovementDetector) computeTrajectory() []TrajectoryPoint {
	if len(cmd.history.Movements) < 2 {
		return nil
	}

	trajectory := make([]TrajectoryPoint, 0, len(cmd.history.Movements))

	// Compute trajectory points based on movement direction and speed
	var cumulativeX, cumulativeY, cumulativeZ float64

	for _, movement := range cmd.history.Movements {
		if movement.Direction != nil {
			// Convert direction to trajectory deltas
			deltaX, deltaY, deltaZ := cmd.directionToDeltas(movement.Direction, movement.Speed)

			cumulativeX += deltaX
			cumulativeY += deltaY
			cumulativeZ += deltaZ

			// Normalize to 0-1 range
			x := math.Max(0, math.Min(1, 0.5+cumulativeX))
			y := math.Max(0, math.Min(1, 0.5+cumulativeY))
			z := cumulativeZ

			trajectory = append(trajectory, TrajectoryPoint{
				X:         x,
				Y:         y,
				Z:         z,
				Timestamp: movement.Timestamp,
			})
		}
	}

	return trajectory
}

// directionToDeltas converts direction and speed to trajectory deltas
func (cmd *CameraMovementDetector) directionToDeltas(dir *MovementDirection, speed MovementSpeed) (float64, float64, float64) {
	// Speed multiplier
	speedMultiplier := 0.0
	switch speed {
	case SpeedStill:
		speedMultiplier = 0.0
	case SpeedVerySlow:
		speedMultiplier = 0.01
	case SpeedSlow:
		speedMultiplier = 0.02
	case SpeedMedium:
		speedMultiplier = 0.05
	case SpeedFast:
		speedMultiplier = 0.10
	case SpeedVeryFast:
		speedMultiplier = 0.20
	}

	// Horizontal delta
	var deltaX float64
	switch dir.Horizontal {
	case "left":
		deltaX = -speedMultiplier
	case "right":
		deltaX = speedMultiplier
	}

	// Vertical delta
	var deltaY float64
	switch dir.Vertical {
	case "up":
		deltaY = -speedMultiplier // Inverted: up is negative Y in screen coords
	case "down":
		deltaY = speedMultiplier
	}

	// Depth delta
	var deltaZ float64
	switch dir.Depth {
	case "in":
		deltaZ = speedMultiplier
	case "out":
		deltaZ = -speedMultiplier
	}

	return deltaX, deltaY, deltaZ
}

// GetMovementStatistics computes statistics across movement history
func (cmd *CameraMovementDetector) GetMovementStatistics() map[string]interface{} {
	if len(cmd.history.Movements) == 0 {
		return map[string]interface{}{
			"total_frames": 0,
		}
	}

	// Count movement types
	typeCounts := make(map[CameraMovementType]int)
	speedCounts := make(map[MovementSpeed]int)
	smoothnessCounts := make(map[MovementSmoothness]int)

	var totalConfidence float64
	var stabilizedCount int
	var shakeCount int

	for _, movement := range cmd.history.Movements {
		typeCounts[movement.PrimaryMovement]++
		speedCounts[movement.Speed]++
		smoothnessCounts[movement.Smoothness]++
		totalConfidence += movement.Confidence

		if movement.Stabilization.IsStabilized {
			stabilizedCount++
		}
		if movement.Shake.HasShake {
			shakeCount++
		}
	}

	// Find dominant types
	var dominantType CameraMovementType
	var maxTypeCount int
	for t, count := range typeCounts {
		if count > maxTypeCount {
			maxTypeCount = count
			dominantType = t
		}
	}

	return map[string]interface{}{
		"total_frames":        len(cmd.history.Movements),
		"movement_types":      typeCounts,
		"dominant_type":       dominantType,
		"speed_distribution":  speedCounts,
		"smoothness_distribution": smoothnessCounts,
		"average_confidence":  totalConfidence / float64(len(cmd.history.Movements)),
		"stabilization_rate":  float64(stabilizedCount) / float64(len(cmd.history.Movements)),
		"shake_rate":          float64(shakeCount) / float64(len(cmd.history.Movements)),
	}
}

// DetectMovementChange detects significant changes in camera movement between frames
func (cmd *CameraMovementDetector) DetectMovementChange(threshold float64) bool {
	if len(cmd.history.Movements) < 2 {
		return false
	}

	current := cmd.history.Movements[len(cmd.history.Movements)-1]
	previous := cmd.history.Movements[len(cmd.history.Movements)-2]

	// Simple change detection: different primary movement type
	if current.PrimaryMovement != previous.PrimaryMovement {
		return true
	}

	// Speed change
	if current.Speed != previous.Speed {
		return true
	}

	// Direction change (if both have direction)
	if current.Direction != nil && previous.Direction != nil {
		if current.Direction.Horizontal != previous.Direction.Horizontal ||
			current.Direction.Vertical != previous.Direction.Vertical {
			return true
		}
	}

	return false
}
