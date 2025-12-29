package tracking

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// TrackerType represents the tracking algorithm
type TrackerType string

const (
	TrackerDeepSORT TrackerType = "deepsort"
	TrackerByteTrack TrackerType = "bytetrack"
	TrackerSimpleIOU TrackerType = "simple_iou"
)

// ObjectClass represents the class of tracked object
type ObjectClass string

const (
	ClassPerson   ObjectClass = "person"
	ClassVehicle  ObjectClass = "vehicle"
	ClassAnimal   ObjectClass = "animal"
	ClassObject   ObjectClass = "object"
	ClassUnknown  ObjectClass = "unknown"
)

// BoundingBox represents object location in frame
type BoundingBox struct {
	X      float64 `json:"x"`      // Top-left X (normalized 0-1)
	Y      float64 `json:"y"`      // Top-left Y (normalized 0-1)
	Width  float64 `json:"width"`  // Width (normalized 0-1)
	Height float64 `json:"height"` // Height (normalized 0-1)
}

// TrackedObject represents a tracked object across frames
type TrackedObject struct {
	TrackID       string                 `json:"trackId"`       // Unique tracking ID
	Class         ObjectClass            `json:"class"`         // Object class
	Confidence    float64                `json:"confidence"`    // Detection confidence (0-1)
	BoundingBox   BoundingBox            `json:"boundingBox"`   // Current bounding box
	Velocity      *Velocity              `json:"velocity"`      // Movement velocity
	FirstSeen     time.Time              `json:"firstSeen"`     // When first detected
	LastSeen      time.Time              `json:"lastSeen"`      // Last detection time
	FrameCount    int                    `json:"frameCount"`    // Frames tracked
	Lost          bool                   `json:"lost"`          // Track lost flag
	LostFrames    int                    `json:"lostFrames"`    // Consecutive lost frames
	Features      []float64              `json:"features"`      // Appearance features for re-ID
	Trajectory    []TrajectoryPoint      `json:"trajectory"`    // Movement trajectory
	State         ObjectState            `json:"state"`         // Object state
	Attributes    map[string]interface{} `json:"attributes"`    // Additional attributes
}

// Velocity represents object movement velocity
type Velocity struct {
	VX float64 `json:"vx"` // X velocity (pixels/frame)
	VY float64 `json:"vy"` // Y velocity (pixels/frame)
	Speed float64 `json:"speed"` // Overall speed
	Direction float64 `json:"direction"` // Movement direction (degrees)
}

// TrajectoryPoint represents a point in object trajectory
type TrajectoryPoint struct {
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	Timestamp time.Time `json:"timestamp"`
	FrameNum  int       `json:"frameNum"`
}

// ObjectState represents object movement state
type ObjectState string

const (
	StateStationary ObjectState = "stationary"
	StateMoving     ObjectState = "moving"
	StateStopped    ObjectState = "stopped"
	StateEntering   ObjectState = "entering"
	StateExiting    ObjectState = "exiting"
)

// Detection represents a single object detection
type Detection struct {
	Class       ObjectClass            `json:"class"`
	Confidence  float64                `json:"confidence"`
	BoundingBox BoundingBox            `json:"boundingBox"`
	Features    []float64              `json:"features"`
	Attributes  map[string]interface{} `json:"attributes"`
}

// TrackingResult represents tracking results for a frame
type TrackingResult struct {
	FrameNum      int                    `json:"frameNum"`
	Timestamp     time.Time              `json:"timestamp"`
	TrackedObjects []TrackedObject       `json:"trackedObjects"`
	NewTracks     int                    `json:"newTracks"`
	LostTracks    int                    `json:"lostTracks"`
	ActiveTracks  int                    `json:"activeTracks"`
	Statistics    TrackingStatistics     `json:"statistics"`
	Attributes    map[string]interface{} `json:"attributes"`
}

// TrackingStatistics provides tracking performance metrics
type TrackingStatistics struct {
	TotalDetections   int     `json:"totalDetections"`
	TotalTracks       int     `json:"totalTracks"`
	AverageTrackLife  float64 `json:"averageTrackLife"`  // Average frames per track
	TrackingAccuracy  float64 `json:"trackingAccuracy"`  // Estimated accuracy (0-1)
	ProcessingTimeMs  float64 `json:"processingTimeMs"`
}

// MultiObjectTracker tracks multiple objects across video frames
type MultiObjectTracker struct {
	trackerType    TrackerType
	mageAgent      *clients.MageAgentClient
	tracks         map[string]*TrackedObject // Active tracks by ID
	nextTrackID    int
	frameNum       int
	maxLostFrames  int     // Max frames before track is removed
	iouThreshold   float64 // IOU threshold for matching
	confidenceThreshold float64 // Min confidence for detections
	maxTracks      int     // Max simultaneous tracks
	mu             sync.RWMutex
}

// NewMultiObjectTracker creates a new multi-object tracker
func NewMultiObjectTracker(trackerType TrackerType, mageAgent *clients.MageAgentClient) *MultiObjectTracker {
	return &MultiObjectTracker{
		trackerType:         trackerType,
		mageAgent:           mageAgent,
		tracks:              make(map[string]*TrackedObject),
		nextTrackID:         1,
		frameNum:            0,
		maxLostFrames:       30,  // 1 second at 30fps
		iouThreshold:        0.3,  // Standard IOU threshold
		confidenceThreshold: 0.5,  // Min 50% confidence
		maxTracks:           100,  // Max 100 simultaneous tracks
	}
}

// Track processes a frame and updates object tracking
func (mot *MultiObjectTracker) Track(ctx context.Context, frameData string) (*TrackingResult, error) {
	startTime := time.Now()
	mot.mu.Lock()
	mot.frameNum++
	currentFrame := mot.frameNum
	mot.mu.Unlock()

	// Detect objects in frame
	detections, err := mot.detectObjects(ctx, frameData)
	if err != nil {
		return nil, fmt.Errorf("object detection failed: %w", err)
	}

	// Match detections to existing tracks
	mot.mu.Lock()
	defer mot.mu.Unlock()

	matches, unmatchedDetections, unmatchedTracks := mot.matchDetectionsToTracks(detections)

	// Update matched tracks
	for trackID, detection := range matches {
		mot.updateTrack(trackID, detection, currentFrame)
	}

	// Create new tracks for unmatched detections
	newTracks := 0
	for _, detection := range unmatchedDetections {
		if len(mot.tracks) < mot.maxTracks {
			mot.createTrack(detection, currentFrame)
			newTracks++
		}
	}

	// Mark unmatched tracks as lost
	lostTracks := 0
	for _, trackID := range unmatchedTracks {
		track := mot.tracks[trackID]
		track.LostFrames++
		if track.LostFrames > mot.maxLostFrames {
			track.Lost = true
			lostTracks++
		}
	}

	// Remove lost tracks
	mot.removeLostTracks()

	// Collect active tracks
	activeTracks := make([]TrackedObject, 0, len(mot.tracks))
	for _, track := range mot.tracks {
		if !track.Lost {
			activeTracks = append(activeTracks, *track)
		}
	}

	// Compute statistics
	stats := mot.computeStatistics(len(detections), len(activeTracks), startTime)

	result := &TrackingResult{
		FrameNum:       currentFrame,
		Timestamp:      startTime,
		TrackedObjects: activeTracks,
		NewTracks:      newTracks,
		LostTracks:     lostTracks,
		ActiveTracks:   len(activeTracks),
		Statistics:     stats,
		Attributes: map[string]interface{}{
			"tracker_type": string(mot.trackerType),
			"total_detections": len(detections),
		},
	}

	log.Printf("Tracking frame %d: %d active tracks, %d new, %d lost",
		currentFrame, len(activeTracks), newTracks, lostTracks)

	return result, nil
}

// detectObjects detects objects in a frame using MageAgent
func (mot *MultiObjectTracker) detectObjects(ctx context.Context, frameData string) ([]Detection, error) {
	prompt := `Detect all people, vehicles, animals, and significant objects in this video frame.
For each detected object, provide:
1. Class: person, vehicle, animal, or object
2. Confidence: 0.0-1.0
3. Bounding box: x, y, width, height (normalized 0-1, from top-left)
4. Attributes: color, size, pose, motion state

Respond with JSON array:
[
  {
    "class": "person|vehicle|animal|object",
    "confidence": 0.0-1.0,
    "boundingBox": {"x": 0-1, "y": 0-1, "width": 0-1, "height": 0-1},
    "attributes": {"color": "...", "size": "small|medium|large", "pose": "...", "motion": "stationary|moving"}
  }
]`

	visionReq := models.MageAgentVisionRequest{
		Image:     frameData,
		Prompt:    prompt,
		MaxTokens: 1000,
	}

	visionResp, err := mot.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Parse detections
	detections, err := mot.parseDetections(visionResp.Description)
	if err != nil {
		log.Printf("Failed to parse detections: %v", err)
		return []Detection{}, nil // Return empty instead of error
	}

	// Filter by confidence threshold
	filtered := make([]Detection, 0)
	for _, det := range detections {
		if det.Confidence >= mot.confidenceThreshold {
			filtered = append(filtered, det)
		}
	}

	return filtered, nil
}

// parseDetections parses AI response into Detection objects
func (mot *MultiObjectTracker) parseDetections(response string) ([]Detection, error) {
	// TODO: Implement JSON parsing from response
	// For now, return empty array
	return []Detection{}, nil
}

// matchDetectionsToTracks matches current detections to existing tracks
func (mot *MultiObjectTracker) matchDetectionsToTracks(detections []Detection) (
	matches map[string]Detection,
	unmatchedDetections []Detection,
	unmatchedTracks []string,
) {
	matches = make(map[string]Detection)
	unmatchedDetections = make([]Detection, 0)
	unmatchedTracks = make([]string, 0)

	// Simple greedy matching based on IOU and class
	usedDetections := make(map[int]bool)

	for trackID, track := range mot.tracks {
		if track.Lost {
			continue
		}

		bestMatch := -1
		bestIOU := mot.iouThreshold

		for i, detection := range detections {
			if usedDetections[i] {
				continue
			}

			// Must match class
			if detection.Class != track.Class {
				continue
			}

			// Compute IOU
			iou := computeIOU(track.BoundingBox, detection.BoundingBox)
			if iou > bestIOU {
				bestIOU = iou
				bestMatch = i
			}
		}

		if bestMatch >= 0 {
			matches[trackID] = detections[bestMatch]
			usedDetections[bestMatch] = true
		} else {
			unmatchedTracks = append(unmatchedTracks, trackID)
		}
	}

	// Collect unmatched detections
	for i, detection := range detections {
		if !usedDetections[i] {
			unmatchedDetections = append(unmatchedDetections, detection)
		}
	}

	return matches, unmatchedDetections, unmatchedTracks
}

// updateTrack updates an existing track with new detection
func (mot *MultiObjectTracker) updateTrack(trackID string, detection Detection, frameNum int) {
	track := mot.tracks[trackID]

	// Update bounding box and velocity
	oldBox := track.BoundingBox
	track.BoundingBox = detection.BoundingBox

	// Compute velocity
	vx := (detection.BoundingBox.X - oldBox.X)
	vy := (detection.BoundingBox.Y - oldBox.Y)
	speed := math.Sqrt(vx*vx + vy*vy)
	direction := math.Atan2(vy, vx) * 180 / math.Pi

	track.Velocity = &Velocity{
		VX:        vx,
		VY:        vy,
		Speed:     speed,
		Direction: direction,
	}

	// Update state
	if speed < 0.01 {
		track.State = StateStationary
	} else {
		track.State = StateMoving
	}

	// Update confidence and timestamps
	track.Confidence = detection.Confidence
	track.LastSeen = time.Now()
	track.FrameCount++
	track.LostFrames = 0

	// Add trajectory point
	track.Trajectory = append(track.Trajectory, TrajectoryPoint{
		X:         detection.BoundingBox.X + detection.BoundingBox.Width/2,
		Y:         detection.BoundingBox.Y + detection.BoundingBox.Height/2,
		Timestamp: time.Now(),
		FrameNum:  frameNum,
	})

	// Limit trajectory size
	if len(track.Trajectory) > 100 {
		track.Trajectory = track.Trajectory[1:]
	}
}

// createTrack creates a new track from detection
func (mot *MultiObjectTracker) createTrack(detection Detection, frameNum int) {
	trackID := fmt.Sprintf("track_%d", mot.nextTrackID)
	mot.nextTrackID++

	now := time.Now()
	track := &TrackedObject{
		TrackID:     trackID,
		Class:       detection.Class,
		Confidence:  detection.Confidence,
		BoundingBox: detection.BoundingBox,
		Velocity:    nil, // No velocity yet
		FirstSeen:   now,
		LastSeen:    now,
		FrameCount:  1,
		Lost:        false,
		LostFrames:  0,
		Features:    detection.Features,
		Trajectory: []TrajectoryPoint{{
			X:         detection.BoundingBox.X + detection.BoundingBox.Width/2,
			Y:         detection.BoundingBox.Y + detection.BoundingBox.Height/2,
			Timestamp: now,
			FrameNum:  frameNum,
		}},
		State:      StateEntering,
		Attributes: detection.Attributes,
	}

	mot.tracks[trackID] = track
}

// removeLostTracks removes tracks that have been lost for too long
func (mot *MultiObjectTracker) removeLostTracks() {
	for trackID, track := range mot.tracks {
		if track.Lost && track.LostFrames > mot.maxLostFrames {
			delete(mot.tracks, trackID)
		}
	}
}

// computeStatistics computes tracking statistics
func (mot *MultiObjectTracker) computeStatistics(numDetections, numActiveTracks int, startTime time.Time) TrackingStatistics {
	totalFrames := 0
	for _, track := range mot.tracks {
		totalFrames += track.FrameCount
	}

	avgTrackLife := 0.0
	if len(mot.tracks) > 0 {
		avgTrackLife = float64(totalFrames) / float64(len(mot.tracks))
	}

	processingTime := time.Since(startTime).Milliseconds()

	return TrackingStatistics{
		TotalDetections:  numDetections,
		TotalTracks:      len(mot.tracks),
		AverageTrackLife: avgTrackLife,
		TrackingAccuracy: 0.85, // Placeholder
		ProcessingTimeMs: float64(processingTime),
	}
}

// GetTrack retrieves a specific track by ID
func (mot *MultiObjectTracker) GetTrack(trackID string) (*TrackedObject, bool) {
	mot.mu.RLock()
	defer mot.mu.RUnlock()
	track, exists := mot.tracks[trackID]
	return track, exists
}

// GetAllTracks returns all active tracks
func (mot *MultiObjectTracker) GetAllTracks() []TrackedObject {
	mot.mu.RLock()
	defer mot.mu.RUnlock()

	tracks := make([]TrackedObject, 0, len(mot.tracks))
	for _, track := range mot.tracks {
		if !track.Lost {
			tracks = append(tracks, *track)
		}
	}
	return tracks
}

// Reset resets the tracker state
func (mot *MultiObjectTracker) Reset() {
	mot.mu.Lock()
	defer mot.mu.Unlock()

	mot.tracks = make(map[string]*TrackedObject)
	mot.nextTrackID = 1
	mot.frameNum = 0
}

// computeIOU computes Intersection over Union between two bounding boxes
func computeIOU(box1, box2 BoundingBox) float64 {
	// Compute intersection
	x1 := math.Max(box1.X, box2.X)
	y1 := math.Max(box1.Y, box2.Y)
	x2 := math.Min(box1.X+box1.Width, box2.X+box2.Width)
	y2 := math.Min(box1.Y+box1.Height, box2.Y+box2.Height)

	if x2 < x1 || y2 < y1 {
		return 0.0 // No intersection
	}

	intersection := (x2 - x1) * (y2 - y1)

	// Compute union
	area1 := box1.Width * box1.Height
	area2 := box2.Width * box2.Height
	union := area1 + area2 - intersection

	if union == 0 {
		return 0.0
	}

	return intersection / union
}
