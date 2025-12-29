package scene

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// SceneType represents different types of scenes
type SceneType string

const (
	SceneTypeInterior  SceneType = "interior"
	SceneTypeExterior  SceneType = "exterior"
	SceneTypeAction    SceneType = "action"
	SceneTypeDialogue  SceneType = "dialogue"
	SceneTypeEstablish SceneType = "establishing"
	SceneTypeMontage   SceneType = "montage"
	SceneTypeTransit   SceneType = "transition"
	SceneTypeCutaway   SceneType = "cutaway"
	SceneTypeUnknown   SceneType = "unknown"
)

// SceneClassification represents the classification result for a scene
type SceneClassification struct {
	PrimaryType   SceneType              `json:"primaryType"`
	SecondaryType *SceneType             `json:"secondaryType,omitempty"`
	Confidence    float64                `json:"confidence"` // 0-1
	Setting       string                 `json:"setting"`
	TimeOfDay     string                 `json:"timeOfDay"`
	Weather       string                 `json:"weather,omitempty"`
	LocationType  string                 `json:"locationType"`
	Attributes    map[string]interface{} `json:"attributes"`
	Timestamp     time.Time              `json:"timestamp"`
}

// SceneClassifier classifies video scenes using AI vision models
type SceneClassifier struct {
	mageAgent *clients.MageAgentClient
}

// NewSceneClassifier creates a new scene classifier
func NewSceneClassifier(mageAgent *clients.MageAgentClient) *SceneClassifier {
	return &SceneClassifier{
		mageAgent: mageAgent,
	}
}

// ClassifyScene analyzes a frame and classifies the scene type
func (sc *SceneClassifier) ClassifyScene(ctx context.Context, frameData string) (*SceneClassification, error) {
	// Prepare vision analysis request
	prompt := `Analyze this video frame and classify the scene with the following details:

1. PRIMARY SCENE TYPE: Choose the best match from:
   - interior: Indoor scenes (rooms, buildings)
   - exterior: Outdoor scenes (streets, nature, open spaces)
   - action: High-energy scenes with movement/activity
   - dialogue: Conversation-focused scenes
   - establishing: Wide shots showing location context
   - montage: Quick cuts showing passage of time
   - transition: Scene changes or transitions
   - cutaway: Brief shots away from main action

2. SETTING: Describe the specific location (e.g., "modern office", "city street", "forest clearing")

3. TIME OF DAY: morning, afternoon, evening, night, or unknown

4. WEATHER (if exterior): sunny, cloudy, rainy, snowy, or unknown

5. LOCATION TYPE: residential, commercial, industrial, natural, urban, rural, etc.

6. KEY ATTRIBUTES: Any notable visual elements (crowds, vehicles, furniture, lighting style, etc.)

Format your response as JSON:
{
  "primaryType": "interior|exterior|action|dialogue|establishing|montage|transition|cutaway",
  "secondaryType": "optional secondary type",
  "confidence": 0.0-1.0,
  "setting": "description",
  "timeOfDay": "morning|afternoon|evening|night|unknown",
  "weather": "sunny|cloudy|rainy|snowy|unknown",
  "locationType": "type",
  "attributes": {
    "key": "value"
  }
}`

	visionReq := models.MageAgentVisionRequest{
		Image:     frameData,
		Prompt:    prompt,
		MaxTokens: 800,
	}

	// Call MageAgent for vision analysis
	visionResp, err := sc.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Parse response
	classification, err := sc.parseClassificationResponse(visionResp.Description)
	if err != nil {
		// Fallback to heuristic classification
		return sc.fallbackClassification(visionResp.Description), nil
	}

	classification.Timestamp = time.Now()
	return classification, nil
}

// ClassifySceneBatch classifies multiple frames efficiently
func (sc *SceneClassifier) ClassifySceneBatch(ctx context.Context, frames []string) ([]*SceneClassification, error) {
	results := make([]*SceneClassification, len(frames))

	// Process frames in parallel (batch of 5)
	batchSize := 5
	for i := 0; i < len(frames); i += batchSize {
		end := i + batchSize
		if end > len(frames) {
			end = len(frames)
		}

		// Process batch
		for j := i; j < end; j++ {
			classification, err := sc.ClassifyScene(ctx, frames[j])
			if err != nil {
				// Continue with other frames on error
				results[j] = &SceneClassification{
					PrimaryType: SceneTypeUnknown,
					Confidence:  0.0,
					Timestamp:   time.Now(),
				}
				continue
			}
			results[j] = classification
		}
	}

	return results, nil
}

// parseClassificationResponse parses AI response into SceneClassification
func (sc *SceneClassifier) parseClassificationResponse(response string) (*SceneClassification, error) {
	// Try to extract JSON from response
	jsonStr := sc.extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result struct {
		PrimaryType   string                 `json:"primaryType"`
		SecondaryType string                 `json:"secondaryType,omitempty"`
		Confidence    float64                `json:"confidence"`
		Setting       string                 `json:"setting"`
		TimeOfDay     string                 `json:"timeOfDay"`
		Weather       string                 `json:"weather,omitempty"`
		LocationType  string                 `json:"locationType"`
		Attributes    map[string]interface{} `json:"attributes"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	classification := &SceneClassification{
		PrimaryType:  SceneType(result.PrimaryType),
		Confidence:   result.Confidence,
		Setting:      result.Setting,
		TimeOfDay:    result.TimeOfDay,
		Weather:      result.Weather,
		LocationType: result.LocationType,
		Attributes:   result.Attributes,
	}

	if result.SecondaryType != "" {
		secondaryType := SceneType(result.SecondaryType)
		classification.SecondaryType = &secondaryType
	}

	// Validate primary type
	if !sc.isValidSceneType(classification.PrimaryType) {
		classification.PrimaryType = SceneTypeUnknown
		classification.Confidence *= 0.5 // Reduce confidence
	}

	return classification, nil
}

// extractJSON extracts JSON string from text
func (sc *SceneClassifier) extractJSON(text string) string {
	// Find JSON block
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")

	if start == -1 || end == -1 || end <= start {
		return ""
	}

	return text[start : end+1]
}

// isValidSceneType checks if scene type is valid
func (sc *SceneClassifier) isValidSceneType(sceneType SceneType) bool {
	validTypes := []SceneType{
		SceneTypeInterior,
		SceneTypeExterior,
		SceneTypeAction,
		SceneTypeDialogue,
		SceneTypeEstablish,
		SceneTypeMontage,
		SceneTypeTransit,
		SceneTypeCutaway,
	}

	for _, valid := range validTypes {
		if sceneType == valid {
			return true
		}
	}
	return false
}

// fallbackClassification provides heuristic classification when AI parsing fails
func (sc *SceneClassifier) fallbackClassification(analysis string) *SceneClassification {
	classification := &SceneClassification{
		PrimaryType: SceneTypeUnknown,
		Confidence:  0.3, // Low confidence for fallback
		Setting:     "unknown",
		TimeOfDay:   "unknown",
		Attributes:  make(map[string]interface{}),
		Timestamp:   time.Now(),
	}

	// Simple keyword-based classification
	lowerAnalysis := strings.ToLower(analysis)

	// Detect scene type
	if strings.Contains(lowerAnalysis, "indoor") || strings.Contains(lowerAnalysis, "room") || strings.Contains(lowerAnalysis, "inside") {
		classification.PrimaryType = SceneTypeInterior
		classification.Confidence = 0.6
	} else if strings.Contains(lowerAnalysis, "outdoor") || strings.Contains(lowerAnalysis, "outside") || strings.Contains(lowerAnalysis, "street") {
		classification.PrimaryType = SceneTypeExterior
		classification.Confidence = 0.6
	}

	// Detect time of day
	if strings.Contains(lowerAnalysis, "night") || strings.Contains(lowerAnalysis, "dark") {
		classification.TimeOfDay = "night"
	} else if strings.Contains(lowerAnalysis, "morning") || strings.Contains(lowerAnalysis, "sunrise") {
		classification.TimeOfDay = "morning"
	} else if strings.Contains(lowerAnalysis, "evening") || strings.Contains(lowerAnalysis, "sunset") {
		classification.TimeOfDay = "evening"
	} else if strings.Contains(lowerAnalysis, "day") || strings.Contains(lowerAnalysis, "sunny") {
		classification.TimeOfDay = "afternoon"
	}

	// Extract setting from analysis
	if len(analysis) > 0 && len(analysis) < 200 {
		classification.Setting = analysis
	}

	return classification
}

// GetSceneTransitions identifies scene transitions from a sequence of classifications
func (sc *SceneClassifier) GetSceneTransitions(classifications []*SceneClassification, threshold float64) []int {
	if len(classifications) < 2 {
		return []int{}
	}

	transitions := []int{}

	for i := 1; i < len(classifications); i++ {
		prev := classifications[i-1]
		curr := classifications[i]

		// Check for scene type change
		if prev.PrimaryType != curr.PrimaryType {
			transitions = append(transitions, i)
			continue
		}

		// Check for setting change
		if prev.Setting != curr.Setting && curr.Confidence > threshold {
			transitions = append(transitions, i)
			continue
		}

		// Check for time of day change
		if prev.TimeOfDay != curr.TimeOfDay && prev.TimeOfDay != "unknown" && curr.TimeOfDay != "unknown" {
			transitions = append(transitions, i)
		}
	}

	return transitions
}

// GetSceneStatistics computes statistics for scene classifications
func (sc *SceneClassifier) GetSceneStatistics(classifications []*SceneClassification) map[string]interface{} {
	stats := make(map[string]interface{})

	// Count by type
	typeCounts := make(map[SceneType]int)
	timeOfDayCounts := make(map[string]int)
	locationTypeCounts := make(map[string]int)

	totalConfidence := 0.0
	validCount := 0

	for _, classification := range classifications {
		typeCounts[classification.PrimaryType]++

		if classification.TimeOfDay != "" && classification.TimeOfDay != "unknown" {
			timeOfDayCounts[classification.TimeOfDay]++
		}

		if classification.LocationType != "" {
			locationTypeCounts[classification.LocationType]++
		}

		if classification.Confidence > 0 {
			totalConfidence += classification.Confidence
			validCount++
		}
	}

	stats["totalScenes"] = len(classifications)
	stats["sceneTypes"] = typeCounts
	stats["timeOfDay"] = timeOfDayCounts
	stats["locationTypes"] = locationTypeCounts

	if validCount > 0 {
		stats["averageConfidence"] = totalConfidence / float64(validCount)
	} else {
		stats["averageConfidence"] = 0.0
	}

	// Find dominant type
	var dominantType SceneType
	maxCount := 0
	for sceneType, count := range typeCounts {
		if count > maxCount {
			maxCount = count
			dominantType = sceneType
		}
	}
	stats["dominantType"] = dominantType

	return stats
}
