package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// PersonIdentity represents a unique person identity
type PersonIdentity struct {
	IdentityID    string                 `json:"identityId"`    // Unique identity ID
	FirstSeen     time.Time              `json:"firstSeen"`     // When first detected
	LastSeen      time.Time              `json:"lastSeen"`      // Last detection
	Appearances   int                    `json:"appearances"`   // Number of appearances
	TrackIDs      []string               `json:"trackIds"`      // Associated track IDs
	Features      []float64              `json:"features"`      // Averaged appearance features
	Attributes    PersonAttributes       `json:"attributes"`    // Physical attributes
	Confidence    float64                `json:"confidence"`    // Identity confidence
	Aliases       []string               `json:"aliases"`       // Alternative names/IDs
	Metadata      map[string]interface{} `json:"metadata"`      // Additional metadata
}

// PersonAttributes represents physical attributes for re-ID
type PersonAttributes struct {
	Height         string   `json:"height"`         // tall, medium, short
	Build          string   `json:"build"`          // slim, average, athletic, heavy
	HairColor      string   `json:"hairColor"`      // black, brown, blonde, red, gray, bald
	HairLength     string   `json:"hairLength"`     // short, medium, long
	ClothingUpper  string   `json:"clothingUpper"`  // Description of upper clothing
	ClothingLower  string   `json:"clothingLower"`  // Description of lower clothing
	ClothingColors []string `json:"clothingColors"` // Primary clothing colors
	Accessories    []string `json:"accessories"`    // Bag, hat, glasses, etc.
	Age            string   `json:"age"`            // child, teen, adult, elderly
	Gender         string   `json:"gender"`         // male, female, unknown
	Pose           string   `json:"pose"`           // standing, sitting, walking, running
}

// ReIDMatch represents a re-identification match
type ReIDMatch struct {
	IdentityID    string    `json:"identityId"`
	TrackID       string    `json:"trackId"`
	Confidence    float64   `json:"confidence"`
	MatchMethod   string    `json:"matchMethod"`   // appearance, attributes, trajectory
	FeatureDist   float64   `json:"featureDist"`   // Feature distance (lower is better)
	AttributeSim  float64   `json:"attributeSim"`  // Attribute similarity (higher is better)
	Timestamp     time.Time `json:"timestamp"`
}

// PersonReID handles person re-identification across frames and tracks
type PersonReID struct {
	mageAgent        *clients.MageAgentClient
	identities       map[string]*PersonIdentity // Known identities
	nextIdentityID   int
	featureThreshold float64 // Feature distance threshold for matching
	minConfidence    float64 // Minimum confidence for re-ID
	maxIdentities    int     // Maximum identities to track
	mu               sync.RWMutex
}

// NewPersonReID creates a new person re-identification system
func NewPersonReID(mageAgent *clients.MageAgentClient) *PersonReID {
	return &PersonReID{
		mageAgent:        mageAgent,
		identities:       make(map[string]*PersonIdentity),
		nextIdentityID:   1,
		featureThreshold: 0.3,  // Cosine distance threshold
		minConfidence:    0.6,  // 60% minimum confidence
		maxIdentities:    1000, // Track up to 1000 unique people
	}
}

// IdentifyPerson identifies or creates an identity for a person track
func (pr *PersonReID) IdentifyPerson(ctx context.Context, track *TrackedObject, frameData string) (*ReIDMatch, error) {
	if track.Class != ClassPerson {
		return nil, fmt.Errorf("track is not a person: %s", track.Class)
	}

	// Extract person features and attributes
	features, attributes, err := pr.extractPersonFeatures(ctx, frameData, track.BoundingBox)
	if err != nil {
		return nil, fmt.Errorf("feature extraction failed: %w", err)
	}

	// Update track with features
	track.Features = features
	track.Attributes["person_attributes"] = attributes

	// Find best matching identity
	pr.mu.Lock()
	defer pr.mu.Unlock()

	bestMatch := pr.findBestMatch(features, attributes)

	if bestMatch != nil {
		// Update existing identity
		identity := pr.identities[bestMatch.IdentityID]
		identity.LastSeen = time.Now()
		identity.Appearances++
		identity.TrackIDs = append(identity.TrackIDs, track.TrackID)

		// Update averaged features
		identity.Features = pr.updateAveragedFeatures(identity.Features, features, identity.Appearances)

		// Update attributes if more confident
		if bestMatch.Confidence > identity.Confidence {
			identity.Attributes = attributes
			identity.Confidence = bestMatch.Confidence
		}

		log.Printf("Re-identified person %s with confidence %.2f (track: %s)",
			bestMatch.IdentityID, bestMatch.Confidence, track.TrackID)

		return bestMatch, nil
	}

	// Create new identity
	if len(pr.identities) < pr.maxIdentities {
		identity := pr.createIdentity(features, attributes, track.TrackID)

		match := &ReIDMatch{
			IdentityID:   identity.IdentityID,
			TrackID:      track.TrackID,
			Confidence:   identity.Confidence,
			MatchMethod:  "new_identity",
			FeatureDist:  0.0,
			AttributeSim: 1.0,
			Timestamp:    time.Now(),
		}

		log.Printf("Created new identity %s (track: %s)", identity.IdentityID, track.TrackID)

		return match, nil
	}

	return nil, fmt.Errorf("maximum identities reached")
}

// extractPersonFeatures extracts visual features and attributes from person crop
func (pr *PersonReID) extractPersonFeatures(ctx context.Context, frameData string, bbox BoundingBox) ([]float64, PersonAttributes, error) {
	prompt := `Analyze this person and extract detailed attributes for re-identification:

1. HEIGHT: tall, medium, short
2. BUILD: slim, average, athletic, heavy
3. HAIR COLOR: black, brown, blonde, red, gray, bald
4. HAIR LENGTH: short, medium, long
5. UPPER CLOTHING: Detailed description (type, color, pattern)
6. LOWER CLOTHING: Detailed description (type, color, pattern)
7. CLOTHING COLORS: List 2-4 primary colors
8. ACCESSORIES: List any bags, hats, glasses, jewelry, etc.
9. AGE: child (0-12), teen (13-19), adult (20-59), elderly (60+)
10. GENDER: male, female, unknown
11. POSE: standing, sitting, walking, running, crouching

Also generate a feature vector (128 dimensions) representing appearance for matching.

Respond with JSON:
{
  "attributes": {
    "height": "tall|medium|short",
    "build": "slim|average|athletic|heavy",
    "hairColor": "black|brown|blonde|red|gray|bald",
    "hairLength": "short|medium|long",
    "clothingUpper": "...",
    "clothingLower": "...",
    "clothingColors": ["color1", "color2"],
    "accessories": ["item1", "item2"],
    "age": "child|teen|adult|elderly",
    "gender": "male|female|unknown",
    "pose": "standing|sitting|walking|running|crouching"
  },
  "features": [128 float values between -1 and 1]
}`

	visionReq := models.MageAgentVisionRequest{
		Image:     frameData,
		Prompt:    prompt,
		MaxTokens: 1200,
	}

	visionResp, err := pr.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, PersonAttributes{}, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Parse response
	features, attributes, err := pr.parsePersonFeatures(visionResp.Description)
	if err != nil {
		log.Printf("Failed to parse person features: %v", err)
		// Return default values
		return pr.generateDefaultFeatures(), PersonAttributes{}, nil
	}

	return features, attributes, nil
}

// parsePersonFeatures parses AI response into features and attributes
func (pr *PersonReID) parsePersonFeatures(response string) ([]float64, PersonAttributes, error) {
	jsonStr := extractJSON(response)

	var parsed struct {
		Attributes PersonAttributes `json:"attributes"`
		Features   []float64        `json:"features"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, PersonAttributes{}, fmt.Errorf("failed to unmarshal: %w", err)
	}

	// Validate features length
	if len(parsed.Features) != 128 {
		return nil, PersonAttributes{}, fmt.Errorf("invalid feature length: %d", len(parsed.Features))
	}

	return parsed.Features, parsed.Attributes, nil
}

// findBestMatch finds the best matching identity for given features
func (pr *PersonReID) findBestMatch(features []float64, attributes PersonAttributes) *ReIDMatch {
	var bestMatch *ReIDMatch
	bestScore := pr.featureThreshold // Must be better than threshold

	for identityID, identity := range pr.identities {
		// Compute feature distance (cosine distance)
		featureDist := pr.computeFeatureDistance(features, identity.Features)

		// Compute attribute similarity
		attrSim := pr.computeAttributeSimilarity(attributes, identity.Attributes)

		// Combined score (70% features, 30% attributes)
		combinedScore := (1.0 - featureDist) * 0.7 + attrSim * 0.3

		if combinedScore > bestScore {
			bestScore = combinedScore
			bestMatch = &ReIDMatch{
				IdentityID:   identityID,
				Confidence:   combinedScore,
				MatchMethod:  "appearance+attributes",
				FeatureDist:  featureDist,
				AttributeSim: attrSim,
				Timestamp:    time.Now(),
			}
		}
	}

	return bestMatch
}

// computeFeatureDistance computes cosine distance between feature vectors
func (pr *PersonReID) computeFeatureDistance(features1, features2 []float64) float64 {
	if len(features1) != len(features2) {
		return 1.0 // Maximum distance
	}

	var dotProduct, norm1, norm2 float64
	for i := range features1 {
		dotProduct += features1[i] * features2[i]
		norm1 += features1[i] * features1[i]
		norm2 += features2[i] * features2[i]
	}

	if norm1 == 0 || norm2 == 0 {
		return 1.0
	}

	cosineSim := dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))

	// Convert similarity to distance
	return 1.0 - cosineSim
}

// computeAttributeSimilarity computes similarity score for attributes
func (pr *PersonReID) computeAttributeSimilarity(attr1, attr2 PersonAttributes) float64 {
	score := 0.0
	total := 0.0

	// Compare each attribute (equal weight)
	if attr1.Height == attr2.Height {
		score += 1.0
	}
	total += 1.0

	if attr1.Build == attr2.Build {
		score += 1.0
	}
	total += 1.0

	if attr1.HairColor == attr2.HairColor {
		score += 1.0
	}
	total += 1.0

	if attr1.HairLength == attr2.HairLength {
		score += 1.0
	}
	total += 1.0

	if attr1.Age == attr2.Age {
		score += 1.0
	}
	total += 1.0

	if attr1.Gender == attr2.Gender {
		score += 1.0
	}
	total += 1.0

	// Clothing colors overlap
	colorOverlap := 0.0
	for _, color1 := range attr1.ClothingColors {
		for _, color2 := range attr2.ClothingColors {
			if color1 == color2 {
				colorOverlap += 1.0
				break
			}
		}
	}
	if len(attr1.ClothingColors) > 0 {
		score += colorOverlap / float64(len(attr1.ClothingColors))
		total += 1.0
	}

	if total == 0 {
		return 0.0
	}

	return score / total
}

// createIdentity creates a new person identity
func (pr *PersonReID) createIdentity(features []float64, attributes PersonAttributes, trackID string) *PersonIdentity {
	identityID := fmt.Sprintf("person_%d", pr.nextIdentityID)
	pr.nextIdentityID++

	now := time.Now()
	identity := &PersonIdentity{
		IdentityID:  identityID,
		FirstSeen:   now,
		LastSeen:    now,
		Appearances: 1,
		TrackIDs:    []string{trackID},
		Features:    features,
		Attributes:  attributes,
		Confidence:  0.8, // Initial confidence
		Aliases:     []string{},
		Metadata:    make(map[string]interface{}),
	}

	pr.identities[identityID] = identity
	return identity
}

// updateAveragedFeatures updates averaged feature vector
func (pr *PersonReID) updateAveragedFeatures(oldFeatures, newFeatures []float64, appearances int) []float64 {
	if len(oldFeatures) != len(newFeatures) {
		return newFeatures
	}

	// Running average
	averaged := make([]float64, len(oldFeatures))
	weight := 1.0 / float64(appearances)

	for i := range oldFeatures {
		averaged[i] = oldFeatures[i]*(1.0-weight) + newFeatures[i]*weight
	}

	return averaged
}

// generateDefaultFeatures generates default feature vector
func (pr *PersonReID) generateDefaultFeatures() []float64 {
	features := make([]float64, 128)
	// Initialize with small random values
	for i := range features {
		features[i] = (float64(i%10) - 5.0) / 10.0
	}
	return features
}

// GetIdentity retrieves an identity by ID
func (pr *PersonReID) GetIdentity(identityID string) (*PersonIdentity, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	identity, exists := pr.identities[identityID]
	return identity, exists
}

// GetAllIdentities returns all known identities
func (pr *PersonReID) GetAllIdentities() []PersonIdentity {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	identities := make([]PersonIdentity, 0, len(pr.identities))
	for _, identity := range pr.identities {
		identities = append(identities, *identity)
	}
	return identities
}

// MergeIdentities merges two identities (when same person has multiple IDs)
func (pr *PersonReID) MergeIdentities(id1, id2 string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	identity1, exists1 := pr.identities[id1]
	identity2, exists2 := pr.identities[id2]

	if !exists1 || !exists2 {
		return fmt.Errorf("one or both identities not found")
	}

	// Merge into identity1
	identity1.LastSeen = maxTime(identity1.LastSeen, identity2.LastSeen)
	identity1.Appearances += identity2.Appearances
	identity1.TrackIDs = append(identity1.TrackIDs, identity2.TrackIDs...)
	identity1.Aliases = append(identity1.Aliases, id2)

	// Average features
	totalAppearances := identity1.Appearances + identity2.Appearances
	for i := range identity1.Features {
		identity1.Features[i] = (identity1.Features[i]*float64(identity1.Appearances) +
			identity2.Features[i]*float64(identity2.Appearances)) / float64(totalAppearances)
	}

	// Remove identity2
	delete(pr.identities, id2)

	log.Printf("Merged identities %s and %s", id1, id2)

	return nil
}

// Reset clears all identities
func (pr *PersonReID) Reset() {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.identities = make(map[string]*PersonIdentity)
	pr.nextIdentityID = 1
}

// GetStatistics returns re-ID statistics
func (pr *PersonReID) GetStatistics() map[string]interface{} {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	totalAppearances := 0
	avgAppearances := 0.0

	for _, identity := range pr.identities {
		totalAppearances += identity.Appearances
	}

	if len(pr.identities) > 0 {
		avgAppearances = float64(totalAppearances) / float64(len(pr.identities))
	}

	return map[string]interface{}{
		"total_identities":   len(pr.identities),
		"total_appearances":  totalAppearances,
		"average_appearances": avgAppearances,
		"feature_threshold":  pr.featureThreshold,
		"min_confidence":     pr.minConfidence,
	}
}

// maxTime returns the later of two times
func maxTime(t1, t2 time.Time) time.Time {
	if t1.After(t2) {
		return t1
	}
	return t2
}
