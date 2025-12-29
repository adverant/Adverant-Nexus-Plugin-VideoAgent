package tracking

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
)

// InteractionType represents the type of interaction between objects
type InteractionType string

const (
	InteractionNone        InteractionType = "none"
	InteractionApproaching InteractionType = "approaching"
	InteractionMeeting     InteractionType = "meeting"
	InteractionFollowing   InteractionType = "following"
	InteractionAvoiding    InteractionType = "avoiding"
	InteractionPassing     InteractionType = "passing"
	InteractionChasing     InteractionType = "chasing"
	InteractionGrouping    InteractionType = "grouping"
	InteractionCollision   InteractionType = "collision"
	InteractionHandoff     InteractionType = "handoff"
	InteractionGesture     InteractionType = "gesture"
)

// Interaction represents a detected interaction between objects
type Interaction struct {
	InteractionID   string                 `json:"interactionId"`
	Type            InteractionType        `json:"type"`
	Participants    []string               `json:"participants"`    // Track IDs involved
	StartTime       time.Time              `json:"startTime"`
	EndTime         *time.Time             `json:"endTime,omitempty"`
	Duration        float64                `json:"duration"`        // Seconds
	Confidence      float64                `json:"confidence"`      // 0-1
	Proximity       float64                `json:"proximity"`       // Distance between objects
	RelativeSpeed   float64                `json:"relativeSpeed"`   // Relative velocity
	Description     string                 `json:"description"`
	Active          bool                   `json:"active"`
	Attributes      map[string]interface{} `json:"attributes"`
}

// InteractionEvent represents a significant interaction event
type InteractionEvent struct {
	EventID       string                 `json:"eventId"`
	Interaction   *Interaction           `json:"interaction"`
	Timestamp     time.Time              `json:"timestamp"`
	FrameNum      int                    `json:"frameNum"`
	EventType     string                 `json:"eventType"`     // started, ongoing, ended
	Significance  float64                `json:"significance"`  // 0-1, how significant
	Attributes    map[string]interface{} `json:"attributes"`
}

// ProximityThreshold defines distance thresholds for interactions
type ProximityThreshold struct {
	Near      float64 // <0.1 normalized distance
	Medium    float64 // 0.1-0.3
	Far       float64 // >0.3
}

// InteractionDetector detects and analyzes interactions between tracked objects
type InteractionDetector struct {
	mageAgent           *clients.MageAgentClient
	interactions        map[string]*Interaction // Active interactions
	history             []Interaction          // Completed interactions
	nextInteractionID   int
	proximityThreshold  ProximityThreshold
	minDuration         time.Duration // Minimum duration to consider
	maxInteractions     int
}

// NewInteractionDetector creates a new interaction detector
func NewInteractionDetector(mageAgent *clients.MageAgentClient) *InteractionDetector {
	return &InteractionDetector{
		mageAgent:    mageAgent,
		interactions: make(map[string]*Interaction),
		history:      make([]Interaction, 0),
		nextInteractionID: 1,
		proximityThreshold: ProximityThreshold{
			Near:   0.1,
			Medium: 0.3,
			Far:    0.5,
		},
		minDuration:     time.Millisecond * 500, // 0.5 seconds
		maxInteractions: 100,
	}
}

// DetectInteractions detects interactions in current frame
func (id *InteractionDetector) DetectInteractions(ctx context.Context, tracks []TrackedObject, frameData string, frameNum int) ([]InteractionEvent, error) {
	events := make([]InteractionEvent, 0)

	// Check all pairs of tracks
	for i := 0; i < len(tracks); i++ {
		for j := i + 1; j < len(tracks); j++ {
			track1 := &tracks[i]
			track2 := &tracks[j]

			// Compute spatial relationship
			proximity := id.computeProximity(track1.BoundingBox, track2.BoundingBox)
			relativeVel := id.computeRelativeVelocity(track1.Velocity, track2.Velocity)

			// Detect interaction type
			interactionType := id.classifyInteraction(track1, track2, proximity, relativeVel)

			if interactionType != InteractionNone {
				// Check if this is a continuation of existing interaction
				existingID := id.findExistingInteraction(track1.TrackID, track2.TrackID)

				if existingID != "" {
					// Update existing interaction
					interaction := id.interactions[existingID]
					interaction.Proximity = proximity
					interaction.RelativeSpeed = relativeVel

					// Check if type changed
					if interaction.Type != interactionType {
						interaction.Type = interactionType
						interaction.Description = id.describeInteraction(interactionType, track1, track2)
					}

					events = append(events, InteractionEvent{
						EventID:      fmt.Sprintf("event_%d_%d", id.nextInteractionID, frameNum),
						Interaction:  interaction,
						Timestamp:    time.Now(),
						FrameNum:     frameNum,
						EventType:    "ongoing",
						Significance: id.computeSignificance(interaction),
						Attributes:   map[string]interface{}{},
					})
				} else {
					// Create new interaction
					interaction := id.createInteraction(interactionType, track1, track2, proximity, relativeVel)

					events = append(events, InteractionEvent{
						EventID:      fmt.Sprintf("event_%d_%d", interaction.InteractionID, frameNum),
						Interaction:  interaction,
						Timestamp:    time.Now(),
						FrameNum:     frameNum,
						EventType:    "started",
						Significance: id.computeSignificance(interaction),
						Attributes:   map[string]interface{}{},
					})

					log.Printf("New interaction detected: %s between %s and %s",
						interactionType, track1.TrackID, track2.TrackID)
				}
			} else {
				// Check if interaction ended
				existingID := id.findExistingInteraction(track1.TrackID, track2.TrackID)
				if existingID != "" {
					interaction := id.interactions[existingID]
					id.endInteraction(interaction)

					events = append(events, InteractionEvent{
						EventID:      fmt.Sprintf("event_%d_%d", interaction.InteractionID, frameNum),
						Interaction:  interaction,
						Timestamp:    time.Now(),
						FrameNum:     frameNum,
						EventType:    "ended",
						Significance: id.computeSignificance(interaction),
						Attributes:   map[string]interface{}{},
					})

					log.Printf("Interaction ended: %s (duration: %.2fs)",
						interaction.InteractionID, interaction.Duration)
				}
			}
		}
	}

	// Check for group interactions (3+ objects)
	groupEvents := id.detectGroupInteractions(tracks, frameNum)
	events = append(events, groupEvents...)

	// Clean up old interactions
	id.cleanupInteractions()

	return events, nil
}

// computeProximity computes normalized distance between two bounding boxes
func (id *InteractionDetector) computeProximity(box1, box2 BoundingBox) float64 {
	// Center points
	cx1 := box1.X + box1.Width/2
	cy1 := box1.Y + box1.Height/2
	cx2 := box2.X + box2.Width/2
	cy2 := box2.Y + box2.Height/2

	// Euclidean distance
	dist := math.Sqrt((cx2-cx1)*(cx2-cx1) + (cy2-cy1)*(cy2-cy1))

	return dist
}

// computeRelativeVelocity computes relative velocity magnitude
func (id *InteractionDetector) computeRelativeVelocity(vel1, vel2 *Velocity) float64 {
	if vel1 == nil || vel2 == nil {
		return 0
	}

	dvx := vel2.VX - vel1.VX
	dvy := vel2.VY - vel1.VY

	return math.Sqrt(dvx*dvx + dvy*dvy)
}

// classifyInteraction classifies interaction type based on spatial and motion features
func (id *InteractionDetector) classifyInteraction(track1, track2 *TrackedObject, proximity, relativeVel float64) InteractionType {
	// Too far apart
	if proximity > id.proximityThreshold.Far {
		return InteractionNone
	}

	// Very close - potential collision or meeting
	if proximity < id.proximityThreshold.Near {
		if relativeVel > 0.1 {
			return InteractionCollision
		}
		return InteractionMeeting
	}

	// Medium distance - analyze velocities
	if track1.Velocity != nil && track2.Velocity != nil {
		// Check if approaching
		if id.areApproaching(track1, track2) {
			if relativeVel > 0.2 {
				return InteractionChasing
			}
			return InteractionApproaching
		}

		// Check if one is following the other
		if id.isFollowing(track1, track2) {
			return InteractionFollowing
		}

		// Check if avoiding
		if id.areAvoiding(track1, track2) {
			return InteractionAvoiding
		}

		// Check if passing
		if relativeVel > 0.15 {
			return InteractionPassing
		}
	}

	return InteractionNone
}

// areApproaching checks if two objects are approaching each other
func (id *InteractionDetector) areApproaching(track1, track2 *TrackedObject) bool {
	if track1.Velocity == nil || track2.Velocity == nil {
		return false
	}

	// Vector from track1 to track2
	dx := (track2.BoundingBox.X + track2.BoundingBox.Width/2) - (track1.BoundingBox.X + track1.BoundingBox.Width/2)
	dy := (track2.BoundingBox.Y + track2.BoundingBox.Height/2) - (track1.BoundingBox.Y + track1.BoundingBox.Height/2)

	// Dot product of velocities with direction vector
	dot1 := track1.Velocity.VX*dx + track1.Velocity.VY*dy
	dot2 := track2.Velocity.VX*(-dx) + track2.Velocity.VY*(-dy)

	// Both moving towards each other
	return dot1 > 0 && dot2 > 0
}

// isFollowing checks if track1 is following track2
func (id *InteractionDetector) isFollowing(track1, track2 *TrackedObject) bool {
	if track1.Velocity == nil || track2.Velocity == nil {
		return false
	}

	// Similar direction and track1 behind track2
	angleDiff := math.Abs(track1.Velocity.Direction - track2.Velocity.Direction)
	if angleDiff > 30 { // More than 30 degrees difference
		return false
	}

	// Check if track1 is behind track2 in direction of motion
	dx := (track2.BoundingBox.X + track2.BoundingBox.Width/2) - (track1.BoundingBox.X + track1.BoundingBox.Width/2)
	dy := (track2.BoundingBox.Y + track2.BoundingBox.Height/2) - (track1.BoundingBox.Y + track1.BoundingBox.Height/2)

	dirX := math.Cos(track2.Velocity.Direction * math.Pi / 180)
	dirY := math.Sin(track2.Velocity.Direction * math.Pi / 180)

	dot := dx*dirX + dy*dirY

	return dot > 0 // track1 is in front direction of track2's motion
}

// areAvoiding checks if objects are avoiding each other
func (id *InteractionDetector) areAvoiding(track1, track2 *TrackedObject) bool {
	if track1.Velocity == nil || track2.Velocity == nil {
		return false
	}

	// Vector from track1 to track2
	dx := (track2.BoundingBox.X + track2.BoundingBox.Width/2) - (track1.BoundingBox.X + track1.BoundingBox.Width/2)
	dy := (track2.BoundingBox.Y + track2.BoundingBox.Height/2) - (track1.BoundingBox.Y + track1.BoundingBox.Height/2)

	// Dot product of velocities with direction vector
	dot1 := track1.Velocity.VX*dx + track1.Velocity.VY*dy
	dot2 := track2.Velocity.VX*(-dx) + track2.Velocity.VY*(-dy)

	// Both moving away from each other
	return dot1 < 0 && dot2 < 0
}

// findExistingInteraction finds active interaction between two tracks
func (id *InteractionDetector) findExistingInteraction(trackID1, trackID2 string) string {
	for interactionID, interaction := range id.interactions {
		if len(interaction.Participants) == 2 &&
			((interaction.Participants[0] == trackID1 && interaction.Participants[1] == trackID2) ||
				(interaction.Participants[0] == trackID2 && interaction.Participants[1] == trackID1)) {
			return interactionID
		}
	}
	return ""
}

// createInteraction creates a new interaction
func (id *InteractionDetector) createInteraction(interactionType InteractionType, track1, track2 *TrackedObject, proximity, relativeVel float64) *Interaction {
	interactionID := fmt.Sprintf("interaction_%d", id.nextInteractionID)
	id.nextInteractionID++

	now := time.Now()
	interaction := &Interaction{
		InteractionID: interactionID,
		Type:          interactionType,
		Participants:  []string{track1.TrackID, track2.TrackID},
		StartTime:     now,
		EndTime:       nil,
		Duration:      0,
		Confidence:    0.8,
		Proximity:     proximity,
		RelativeSpeed: relativeVel,
		Description:   id.describeInteraction(interactionType, track1, track2),
		Active:        true,
		Attributes: map[string]interface{}{
			"classes": []ObjectClass{track1.Class, track2.Class},
		},
	}

	id.interactions[interactionID] = interaction
	return interaction
}

// describeInteraction generates a human-readable description
func (id *InteractionDetector) describeInteraction(interactionType InteractionType, track1, track2 *TrackedObject) string {
	switch interactionType {
	case InteractionApproaching:
		return fmt.Sprintf("%s and %s are approaching each other", track1.Class, track2.Class)
	case InteractionMeeting:
		return fmt.Sprintf("%s and %s are meeting", track1.Class, track2.Class)
	case InteractionFollowing:
		return fmt.Sprintf("%s is following %s", track1.Class, track2.Class)
	case InteractionAvoiding:
		return fmt.Sprintf("%s and %s are avoiding each other", track1.Class, track2.Class)
	case InteractionPassing:
		return fmt.Sprintf("%s and %s are passing by", track1.Class, track2.Class)
	case InteractionChasing:
		return fmt.Sprintf("%s is chasing %s", track1.Class, track2.Class)
	case InteractionCollision:
		return fmt.Sprintf("%s and %s are colliding", track1.Class, track2.Class)
	default:
		return fmt.Sprintf("Interaction between %s and %s", track1.Class, track2.Class)
	}
}

// endInteraction marks an interaction as ended
func (id *InteractionDetector) endInteraction(interaction *Interaction) {
	now := time.Now()
	interaction.EndTime = &now
	interaction.Duration = now.Sub(interaction.StartTime).Seconds()
	interaction.Active = false

	// Move to history if duration meets minimum
	if interaction.Duration >= id.minDuration.Seconds() {
		id.history = append(id.history, *interaction)
	}

	delete(id.interactions, interaction.InteractionID)
}

// detectGroupInteractions detects interactions involving 3+ objects
func (id *InteractionDetector) detectGroupInteractions(tracks []TrackedObject, frameNum int) []InteractionEvent {
	events := make([]InteractionEvent, 0)

	// Find clusters of nearby objects
	clusters := id.findClusters(tracks)

	for _, cluster := range clusters {
		if len(cluster) >= 3 {
			// Check if this is an existing group interaction
			trackIDs := make([]string, len(cluster))
			for i, idx := range cluster {
				trackIDs[i] = tracks[idx].TrackID
			}

			existingID := id.findGroupInteraction(trackIDs)

			if existingID == "" {
				// Create new group interaction
				interaction := id.createGroupInteraction(tracks, cluster)

				events = append(events, InteractionEvent{
					EventID:      fmt.Sprintf("event_%s_%d", interaction.InteractionID, frameNum),
					Interaction:  interaction,
					Timestamp:    time.Now(),
					FrameNum:     frameNum,
					EventType:    "started",
					Significance: id.computeSignificance(interaction),
					Attributes:   map[string]interface{}{"group_size": len(cluster)},
				})

				log.Printf("New group interaction: %d objects", len(cluster))
			}
		}
	}

	return events
}

// findClusters finds clusters of nearby objects using simple distance-based clustering
func (id *InteractionDetector) findClusters(tracks []TrackedObject) [][]int {
	clusters := make([][]int, 0)
	assigned := make(map[int]bool)

	for i := range tracks {
		if assigned[i] {
			continue
		}

		cluster := []int{i}
		assigned[i] = true

		// Find all tracks within threshold distance
		for j := range tracks {
			if i == j || assigned[j] {
				continue
			}

			proximity := id.computeProximity(tracks[i].BoundingBox, tracks[j].BoundingBox)
			if proximity < id.proximityThreshold.Medium {
				cluster = append(cluster, j)
				assigned[j] = true
			}
		}

		if len(cluster) >= 3 {
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

// findGroupInteraction finds existing group interaction
func (id *InteractionDetector) findGroupInteraction(trackIDs []string) string {
	for interactionID, interaction := range id.interactions {
		if len(interaction.Participants) >= 3 {
			// Check if participants match
			match := true
			for _, trackID := range trackIDs {
				found := false
				for _, p := range interaction.Participants {
					if p == trackID {
						found = true
						break
					}
				}
				if !found {
					match = false
					break
				}
			}
			if match {
				return interactionID
			}
		}
	}
	return ""
}

// createGroupInteraction creates a new group interaction
func (id *InteractionDetector) createGroupInteraction(tracks []TrackedObject, clusterIndices []int) *Interaction {
	interactionID := fmt.Sprintf("interaction_%d", id.nextInteractionID)
	id.nextInteractionID++

	participants := make([]string, len(clusterIndices))
	classes := make([]ObjectClass, len(clusterIndices))

	for i, idx := range clusterIndices {
		participants[i] = tracks[idx].TrackID
		classes[i] = tracks[idx].Class
	}

	now := time.Now()
	interaction := &Interaction{
		InteractionID: interactionID,
		Type:          InteractionGrouping,
		Participants:  participants,
		StartTime:     now,
		EndTime:       nil,
		Duration:      0,
		Confidence:    0.75,
		Proximity:     0.2, // Average proximity
		RelativeSpeed: 0,
		Description:   fmt.Sprintf("Group of %d objects", len(participants)),
		Active:        true,
		Attributes: map[string]interface{}{
			"group_size": len(participants),
			"classes":    classes,
		},
	}

	id.interactions[interactionID] = interaction
	return interaction
}

// computeSignificance computes interaction significance (0-1)
func (id *InteractionDetector) computeSignificance(interaction *Interaction) float64 {
	significance := 0.5 // Base significance

	// Increase for certain types
	switch interaction.Type {
	case InteractionCollision:
		significance = 0.95
	case InteractionChasing:
		significance = 0.85
	case InteractionMeeting:
		significance = 0.75
	case InteractionGrouping:
		// More objects = more significant
		groupSize := len(interaction.Participants)
		significance = 0.6 + float64(groupSize)*0.05
		if significance > 0.95 {
			significance = 0.95
		}
	case InteractionFollowing:
		significance = 0.65
	case InteractionApproaching:
		significance = 0.55
	default:
		significance = 0.4
	}

	// Adjust for duration (longer = more significant)
	if interaction.Duration > 5.0 {
		significance *= 1.1
		if significance > 1.0 {
			significance = 1.0
		}
	}

	return significance
}

// cleanupInteractions removes stale interactions
func (id *InteractionDetector) cleanupInteractions() {
	now := time.Now()
	for interactionID, interaction := range id.interactions {
		// Remove if inactive for too long (5 seconds)
		if now.Sub(interaction.StartTime) > time.Second*5 {
			id.endInteraction(interaction)
			delete(id.interactions, interactionID)
		}
	}

	// Limit history size
	if len(id.history) > 1000 {
		id.history = id.history[len(id.history)-1000:]
	}
}

// GetActiveInteractions returns all active interactions
func (id *InteractionDetector) GetActiveInteractions() []Interaction {
	interactions := make([]Interaction, 0, len(id.interactions))
	for _, interaction := range id.interactions {
		interactions = append(interactions, *interaction)
	}
	return interactions
}

// GetHistory returns interaction history
func (id *InteractionDetector) GetHistory(limit int) []Interaction {
	if limit <= 0 || limit > len(id.history) {
		return id.history
	}
	return id.history[len(id.history)-limit:]
}

// GetStatistics returns interaction statistics
func (id *InteractionDetector) GetStatistics() map[string]interface{} {
	typeCounts := make(map[InteractionType]int)
	for _, interaction := range id.interactions {
		typeCounts[interaction.Type]++
	}

	avgDuration := 0.0
	if len(id.history) > 0 {
		totalDuration := 0.0
		for _, interaction := range id.history {
			totalDuration += interaction.Duration
		}
		avgDuration = totalDuration / float64(len(id.history))
	}

	return map[string]interface{}{
		"active_interactions": len(id.interactions),
		"total_history":       len(id.history),
		"type_counts":         typeCounts,
		"average_duration":    avgDuration,
	}
}

// Reset clears all interactions
func (id *InteractionDetector) Reset() {
	id.interactions = make(map[string]*Interaction)
	id.history = make([]Interaction, 0)
	id.nextInteractionID = 1
}
