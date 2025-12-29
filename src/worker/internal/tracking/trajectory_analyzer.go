package tracking

import (
	"fmt"
	"math"
	"time"
)

// TrajectoryAnalyzer analyzes and predicts object trajectories
type TrajectoryAnalyzer struct {
	predictionHorizon int     // Frames to predict ahead
	smoothingWindow   int     // Window for trajectory smoothing
	minPoints         int     // Minimum points for analysis
	confidenceDecay   float64 // Confidence decay per predicted frame
}

// NewTrajectoryAnalyzer creates a new trajectory analyzer
func NewTrajectoryAnalyzer() *TrajectoryAnalyzer {
	return &TrajectoryAnalyzer{
		predictionHorizon: 30,   // Predict 1 second ahead at 30fps
		smoothingWindow:   5,    // 5-frame smoothing
		minPoints:         3,    // Need at least 3 points
		confidenceDecay:   0.05, // 5% confidence loss per frame
	}
}

// TrajectoryAnalysis represents trajectory analysis results
type TrajectoryAnalysis struct {
	TrackID           string                 `json:"trackId"`
	CurrentPosition   Point2D                `json:"currentPosition"`
	CurrentVelocity   Velocity2D             `json:"currentVelocity"`
	CurrentAccel      Acceleration2D         `json:"currentAcceleration"`
	Predictions       []PredictedPosition    `json:"predictions"`
	Pattern           TrajectoryPattern      `json:"pattern"`
	Smoothness        float64                `json:"smoothness"`        // 0-1, higher is smoother
	Predictability    float64                `json:"predictability"`    // 0-1, higher is more predictable
	TotalDistance     float64                `json:"totalDistance"`     // Total distance traveled
	AverageSpeed      float64                `json:"averageSpeed"`      // Average speed
	Direction         float64                `json:"direction"`         // Overall direction (degrees)
	Curvature         float64                `json:"curvature"`         // Path curvature
	Attributes        map[string]interface{} `json:"attributes"`
}

// Point2D represents a 2D point
type Point2D struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Velocity2D represents 2D velocity
type Velocity2D struct {
	VX    float64 `json:"vx"`
	VY    float64 `json:"vy"`
	Speed float64 `json:"speed"`
	Angle float64 `json:"angle"` // degrees
}

// Acceleration2D represents 2D acceleration
type Acceleration2D struct {
	AX         float64 `json:"ax"`
	AY         float64 `json:"ay"`
	Magnitude  float64 `json:"magnitude"`
}

// PredictedPosition represents a predicted future position
type PredictedPosition struct {
	Position   Point2D   `json:"position"`
	Velocity   Velocity2D `json:"velocity"`
	Confidence float64   `json:"confidence"` // 0-1
	FrameNum   int       `json:"frameNum"`   // Future frame number
	Timestamp  time.Time `json:"timestamp"`  // Predicted time
}

// TrajectoryPattern represents detected movement pattern
type TrajectoryPattern string

const (
	PatternLinear      TrajectoryPattern = "linear"        // Straight line
	PatternCurved      TrajectoryPattern = "curved"        // Smooth curve
	PatternZigzag      TrajectoryPattern = "zigzag"        // Back and forth
	PatternCircular    TrajectoryPattern = "circular"      // Circular motion
	PatternStationary  TrajectoryPattern = "stationary"    // Not moving
	PatternErratic     TrajectoryPattern = "erratic"       // Unpredictable
	PatternAccel       TrajectoryPattern = "accelerating"  // Speeding up
	PatternDecel       TrajectoryPattern = "decelerating"  // Slowing down
)

// AnalyzeTrajectory analyzes a tracked object's trajectory
func (ta *TrajectoryAnalyzer) AnalyzeTrajectory(track *TrackedObject, currentFrame int) (*TrajectoryAnalysis, error) {
	if len(track.Trajectory) < ta.minPoints {
		return nil, fmt.Errorf("insufficient trajectory points: need %d, have %d",
			ta.minPoints, len(track.Trajectory))
	}

	// Get current position and velocity
	currentPos := ta.getCurrentPosition(track.Trajectory)
	currentVel := ta.computeVelocity(track.Trajectory)
	currentAccel := ta.computeAcceleration(track.Trajectory)

	// Smooth trajectory
	smoothed := ta.smoothTrajectory(track.Trajectory)

	// Detect pattern
	pattern := ta.detectPattern(smoothed, currentVel, currentAccel)

	// Compute trajectory metrics
	totalDist := ta.computeTotalDistance(smoothed)
	avgSpeed := ta.computeAverageSpeed(smoothed)
	direction := ta.computeOverallDirection(smoothed)
	curvature := ta.computeCurvature(smoothed)
	smoothness := ta.computeSmoothness(smoothed)
	predictability := ta.computePredictability(smoothed, pattern)

	// Generate predictions
	predictions := ta.predictFuturePositions(smoothed, currentVel, currentAccel, currentFrame, pattern)

	analysis := &TrajectoryAnalysis{
		TrackID:         track.TrackID,
		CurrentPosition: currentPos,
		CurrentVelocity: currentVel,
		CurrentAccel:    currentAccel,
		Predictions:     predictions,
		Pattern:         pattern,
		Smoothness:      smoothness,
		Predictability:  predictability,
		TotalDistance:   totalDist,
		AverageSpeed:    avgSpeed,
		Direction:       direction,
		Curvature:       curvature,
		Attributes: map[string]interface{}{
			"trajectory_points": len(track.Trajectory),
			"smoothed_points":   len(smoothed),
		},
	}

	return analysis, nil
}

// getCurrentPosition gets the most recent position
func (ta *TrajectoryAnalyzer) getCurrentPosition(trajectory []TrajectoryPoint) Point2D {
	if len(trajectory) == 0 {
		return Point2D{X: 0, Y: 0}
	}
	last := trajectory[len(trajectory)-1]
	return Point2D{X: last.X, Y: last.Y}
}

// computeVelocity computes current velocity from recent trajectory
func (ta *TrajectoryAnalyzer) computeVelocity(trajectory []TrajectoryPoint) Velocity2D {
	if len(trajectory) < 2 {
		return Velocity2D{VX: 0, VY: 0, Speed: 0, Angle: 0}
	}

	// Use last N points for velocity estimation
	n := int(math.Min(float64(ta.smoothingWindow), float64(len(trajectory))))
	start := trajectory[len(trajectory)-n]
	end := trajectory[len(trajectory)-1]

	dt := end.Timestamp.Sub(start.Timestamp).Seconds()
	if dt == 0 {
		return Velocity2D{VX: 0, VY: 0, Speed: 0, Angle: 0}
	}

	vx := (end.X - start.X) / dt
	vy := (end.Y - start.Y) / dt
	speed := math.Sqrt(vx*vx + vy*vy)
	angle := math.Atan2(vy, vx) * 180 / math.Pi

	return Velocity2D{VX: vx, VY: vy, Speed: speed, Angle: angle}
}

// computeAcceleration computes current acceleration
func (ta *TrajectoryAnalyzer) computeAcceleration(trajectory []TrajectoryPoint) Acceleration2D {
	if len(trajectory) < 3 {
		return Acceleration2D{AX: 0, AY: 0, Magnitude: 0}
	}

	n := int(math.Min(float64(ta.smoothingWindow), float64(len(trajectory))))

	// Compute velocity at two points
	mid := len(trajectory) - n/2
	v1 := ta.computeVelocityBetween(trajectory[len(trajectory)-n], trajectory[mid])
	v2 := ta.computeVelocityBetween(trajectory[mid], trajectory[len(trajectory)-1])

	dt := trajectory[len(trajectory)-1].Timestamp.Sub(trajectory[mid].Timestamp).Seconds()
	if dt == 0 {
		return Acceleration2D{AX: 0, AY: 0, Magnitude: 0}
	}

	ax := (v2.VX - v1.VX) / dt
	ay := (v2.VY - v1.VY) / dt
	mag := math.Sqrt(ax*ax + ay*ay)

	return Acceleration2D{AX: ax, AY: ay, Magnitude: mag}
}

// computeVelocityBetween computes velocity between two points
func (ta *TrajectoryAnalyzer) computeVelocityBetween(p1, p2 TrajectoryPoint) Velocity2D {
	dt := p2.Timestamp.Sub(p1.Timestamp).Seconds()
	if dt == 0 {
		return Velocity2D{}
	}

	vx := (p2.X - p1.X) / dt
	vy := (p2.Y - p1.Y) / dt
	speed := math.Sqrt(vx*vx + vy*vy)
	angle := math.Atan2(vy, vx) * 180 / math.Pi

	return Velocity2D{VX: vx, VY: vy, Speed: speed, Angle: angle}
}

// smoothTrajectory applies moving average smoothing
func (ta *TrajectoryAnalyzer) smoothTrajectory(trajectory []TrajectoryPoint) []TrajectoryPoint {
	if len(trajectory) <= ta.smoothingWindow {
		return trajectory
	}

	smoothed := make([]TrajectoryPoint, len(trajectory))
	halfWindow := ta.smoothingWindow / 2

	for i := range trajectory {
		start := int(math.Max(0, float64(i-halfWindow)))
		end := int(math.Min(float64(len(trajectory)), float64(i+halfWindow+1)))

		sumX, sumY := 0.0, 0.0
		count := end - start

		for j := start; j < end; j++ {
			sumX += trajectory[j].X
			sumY += trajectory[j].Y
		}

		smoothed[i] = TrajectoryPoint{
			X:         sumX / float64(count),
			Y:         sumY / float64(count),
			Timestamp: trajectory[i].Timestamp,
			FrameNum:  trajectory[i].FrameNum,
		}
	}

	return smoothed
}

// detectPattern detects trajectory pattern
func (ta *TrajectoryAnalyzer) detectPattern(trajectory []TrajectoryPoint, vel Velocity2D, accel Acceleration2D) TrajectoryPattern {
	if vel.Speed < 0.01 {
		return PatternStationary
	}

	// Check for acceleration/deceleration
	if accel.Magnitude > 0.1 {
		if accel.AX*vel.VX+accel.AY*vel.VY > 0 {
			return PatternAccel
		}
		return PatternDecel
	}

	// Compute direction variance
	directionVar := ta.computeDirectionVariance(trajectory)

	if directionVar < 10 {
		return PatternLinear
	} else if directionVar < 30 {
		return PatternCurved
	} else if directionVar < 60 {
		return PatternZigzag
	} else if ta.isCircular(trajectory) {
		return PatternCircular
	}

	return PatternErratic
}

// computeDirectionVariance computes variance in movement direction
func (ta *TrajectoryAnalyzer) computeDirectionVariance(trajectory []TrajectoryPoint) float64 {
	if len(trajectory) < 3 {
		return 0
	}

	directions := make([]float64, 0, len(trajectory)-1)
	for i := 1; i < len(trajectory); i++ {
		dx := trajectory[i].X - trajectory[i-1].X
		dy := trajectory[i].Y - trajectory[i-1].Y
		angle := math.Atan2(dy, dx) * 180 / math.Pi
		directions = append(directions, angle)
	}

	// Compute variance
	mean := 0.0
	for _, dir := range directions {
		mean += dir
	}
	mean /= float64(len(directions))

	variance := 0.0
	for _, dir := range directions {
		diff := dir - mean
		variance += diff * diff
	}
	variance /= float64(len(directions))

	return math.Sqrt(variance)
}

// isCircular checks if trajectory forms a circle
func (ta *TrajectoryAnalyzer) isCircular(trajectory []TrajectoryPoint) bool {
	if len(trajectory) < 10 {
		return false
	}

	// Check if start and end are close
	start := trajectory[0]
	end := trajectory[len(trajectory)-1]
	dist := math.Sqrt((end.X-start.X)*(end.X-start.X) + (end.Y-start.Y)*(end.Y-start.Y))

	// Total path length
	totalDist := ta.computeTotalDistance(trajectory)

	// Circular if end is close to start but path is long
	return dist < totalDist*0.2 && totalDist > 0.5
}

// computeTotalDistance computes total path length
func (ta *TrajectoryAnalyzer) computeTotalDistance(trajectory []TrajectoryPoint) float64 {
	if len(trajectory) < 2 {
		return 0
	}

	totalDist := 0.0
	for i := 1; i < len(trajectory); i++ {
		dx := trajectory[i].X - trajectory[i-1].X
		dy := trajectory[i].Y - trajectory[i-1].Y
		totalDist += math.Sqrt(dx*dx + dy*dy)
	}

	return totalDist
}

// computeAverageSpeed computes average speed
func (ta *TrajectoryAnalyzer) computeAverageSpeed(trajectory []TrajectoryPoint) float64 {
	if len(trajectory) < 2 {
		return 0
	}

	totalDist := ta.computeTotalDistance(trajectory)
	totalTime := trajectory[len(trajectory)-1].Timestamp.Sub(trajectory[0].Timestamp).Seconds()

	if totalTime == 0 {
		return 0
	}

	return totalDist / totalTime
}

// computeOverallDirection computes overall movement direction
func (ta *TrajectoryAnalyzer) computeOverallDirection(trajectory []TrajectoryPoint) float64 {
	if len(trajectory) < 2 {
		return 0
	}

	start := trajectory[0]
	end := trajectory[len(trajectory)-1]

	dx := end.X - start.X
	dy := end.Y - start.Y

	return math.Atan2(dy, dx) * 180 / math.Pi
}

// computeCurvature computes path curvature
func (ta *TrajectoryAnalyzer) computeCurvature(trajectory []TrajectoryPoint) float64 {
	if len(trajectory) < 3 {
		return 0
	}

	// Compute average curvature using three-point method
	totalCurvature := 0.0
	count := 0

	for i := 1; i < len(trajectory)-1; i++ {
		p1 := trajectory[i-1]
		p2 := trajectory[i]
		p3 := trajectory[i+1]

		// Menger curvature
		area := math.Abs((p2.X-p1.X)*(p3.Y-p1.Y) - (p3.X-p1.X)*(p2.Y-p1.Y)) / 2
		d12 := math.Sqrt((p2.X-p1.X)*(p2.X-p1.X) + (p2.Y-p1.Y)*(p2.Y-p1.Y))
		d23 := math.Sqrt((p3.X-p2.X)*(p3.X-p2.X) + (p3.Y-p2.Y)*(p3.Y-p2.Y))
		d31 := math.Sqrt((p1.X-p3.X)*(p1.X-p3.X) + (p1.Y-p3.Y)*(p1.Y-p3.Y))

		if d12*d23*d31 > 0 {
			curvature := 4 * area / (d12 * d23 * d31)
			totalCurvature += curvature
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return totalCurvature / float64(count)
}

// computeSmoothness computes trajectory smoothness (0-1)
func (ta *TrajectoryAnalyzer) computeSmoothness(trajectory []TrajectoryPoint) float64 {
	if len(trajectory) < 3 {
		return 1.0
	}

	// Compute acceleration variance (lower is smoother)
	accelerations := make([]float64, 0, len(trajectory)-2)

	for i := 1; i < len(trajectory)-1; i++ {
		v1 := ta.computeVelocityBetween(trajectory[i-1], trajectory[i])
		v2 := ta.computeVelocityBetween(trajectory[i], trajectory[i+1])

		dt := trajectory[i+1].Timestamp.Sub(trajectory[i].Timestamp).Seconds()
		if dt > 0 {
			accel := math.Sqrt((v2.VX-v1.VX)*(v2.VX-v1.VX) + (v2.VY-v1.VY)*(v2.VY-v1.VY)) / dt
			accelerations = append(accelerations, accel)
		}
	}

	if len(accelerations) == 0 {
		return 1.0
	}

	// Compute variance
	mean := 0.0
	for _, a := range accelerations {
		mean += a
	}
	mean /= float64(len(accelerations))

	variance := 0.0
	for _, a := range accelerations {
		diff := a - mean
		variance += diff * diff
	}
	variance /= float64(len(accelerations))

	// Convert to smoothness score (0-1)
	smoothness := 1.0 / (1.0 + variance)
	return smoothness
}

// computePredictability computes how predictable the trajectory is (0-1)
func (ta *TrajectoryAnalyzer) computePredictability(trajectory []TrajectoryPoint, pattern TrajectoryPattern) float64 {
	// Base predictability on pattern
	switch pattern {
	case PatternLinear:
		return 0.95
	case PatternCurved:
		return 0.85
	case PatternAccel, PatternDecel:
		return 0.75
	case PatternCircular:
		return 0.80
	case PatternZigzag:
		return 0.50
	case PatternStationary:
		return 1.0
	case PatternErratic:
		return 0.30
	default:
		return 0.50
	}
}

// predictFuturePositions predicts future positions
func (ta *TrajectoryAnalyzer) predictFuturePositions(trajectory []TrajectoryPoint, vel Velocity2D, accel Acceleration2D, currentFrame int, pattern TrajectoryPattern) []PredictedPosition {
	predictions := make([]PredictedPosition, 0, ta.predictionHorizon)

	if len(trajectory) == 0 {
		return predictions
	}

	currentPos := trajectory[len(trajectory)-1]
	currentTime := currentPos.Timestamp

	// Frame time (assume 30fps)
	frameTime := time.Millisecond * 33

	confidence := ta.computePredictability(trajectory, pattern)

	for i := 1; i <= ta.predictionHorizon; i++ {
		dt := float64(i) * frameTime.Seconds()

		// Simple kinematic prediction: p = p0 + v*t + 0.5*a*t^2
		predX := currentPos.X + vel.VX*dt + 0.5*accel.AX*dt*dt
		predY := currentPos.Y + vel.VY*dt + 0.5*accel.AY*dt*dt

		// Update velocity: v = v0 + a*t
		predVX := vel.VX + accel.AX*dt
		predVY := vel.VY + accel.AY*dt
		predSpeed := math.Sqrt(predVX*predVX + predVY*predVY)
		predAngle := math.Atan2(predVY, predVX) * 180 / math.Pi

		// Decay confidence with time
		predConfidence := confidence * math.Pow(1.0-ta.confidenceDecay, float64(i))

		prediction := PredictedPosition{
			Position: Point2D{X: predX, Y: predY},
			Velocity: Velocity2D{
				VX:    predVX,
				VY:    predVY,
				Speed: predSpeed,
				Angle: predAngle,
			},
			Confidence: predConfidence,
			FrameNum:   currentFrame + i,
			Timestamp:  currentTime.Add(time.Duration(i) * frameTime),
		}

		predictions = append(predictions, prediction)
	}

	return predictions
}
