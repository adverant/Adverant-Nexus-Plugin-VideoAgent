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

// ShotSize represents the framing/shot size
type ShotSize string

const (
	ShotSizeExtremeCloseUp ShotSize = "extreme_close_up" // ECU
	ShotSizeCloseUp        ShotSize = "close_up"         // CU
	ShotSizeMediumCloseUp  ShotSize = "medium_close_up"  // MCU
	ShotSizeMediumShot     ShotSize = "medium_shot"      // MS
	ShotSizeMediumWide     ShotSize = "medium_wide"      // MW
	ShotSizeWideShot       ShotSize = "wide_shot"        // WS
	ShotSizeExtremeWide    ShotSize = "extreme_wide"     // EWS
	ShotSizeUnknown        ShotSize = "unknown"
)

// ShotAngle represents the camera angle
type ShotAngle string

const (
	ShotAngleEyeLevel  ShotAngle = "eye_level"
	ShotAngleHighAngle ShotAngle = "high_angle"
	ShotAngleLowAngle  ShotAngle = "low_angle"
	ShotAngleBirdsEye  ShotAngle = "birds_eye"
	ShotAngleDutch     ShotAngle = "dutch"  // Tilted
	ShotAngleUnknown   ShotAngle = "unknown"
)

// ShotComposition represents the composition analysis of a shot
type ShotComposition struct {
	ShotSize           ShotSize               `json:"shotSize"`
	ShotAngle          ShotAngle              `json:"shotAngle"`
	RuleOfThirds       bool                   `json:"ruleOfThirds"`
	LeadingLines       bool                   `json:"leadingLines"`
	Symmetry           bool                   `json:"symmetry"`
	Depth              string                 `json:"depth"` // shallow, moderate, deep
	FocalPoint         string                 `json:"focalPoint"`
	SubjectPlacement   string                 `json:"subjectPlacement"`
	BackgroundElements []string               `json:"backgroundElements"`
	ForegroundElements []string               `json:"foregroundElements"`
	ColorPalette       []string               `json:"colorPalette"`
	Contrast           string                 `json:"contrast"` // low, medium, high
	Balance            string                 `json:"balance"`  // balanced, unbalanced, asymmetric
	Confidence         float64                `json:"confidence"`
	Attributes         map[string]interface{} `json:"attributes"`
	Timestamp          time.Time              `json:"timestamp"`
}

// ShotCompositionAnalyzer analyzes shot composition using AI vision
type ShotCompositionAnalyzer struct {
	mageAgent *clients.MageAgentClient
}

// NewShotCompositionAnalyzer creates a new shot composition analyzer
func NewShotCompositionAnalyzer(mageAgent *clients.MageAgentClient) *ShotCompositionAnalyzer {
	return &ShotCompositionAnalyzer{
		mageAgent: mageAgent,
	}
}

// AnalyzeComposition analyzes the composition of a video frame
func (sca *ShotCompositionAnalyzer) AnalyzeComposition(ctx context.Context, frameData string) (*ShotComposition, error) {
	prompt := `Analyze this video frame's cinematographic composition with professional detail:

1. SHOT SIZE (framing):
   - extreme_close_up: Very tight on subject details (eyes, hands)
   - close_up: Subject's head and shoulders
   - medium_close_up: Chest up
   - medium_shot: Waist up
   - medium_wide: Full body with some surroundings
   - wide_shot: Subject in context of environment
   - extreme_wide: Expansive landscape or cityscape

2. SHOT ANGLE (camera position):
   - eye_level: Camera at subject's eye level
   - high_angle: Camera looking down
   - low_angle: Camera looking up
   - birds_eye: Directly overhead
   - dutch: Tilted/canted angle

3. COMPOSITION TECHNIQUES:
   - ruleOfThirds: Subject on power points (true/false)
   - leadingLines: Lines directing eye to subject (true/false)
   - symmetry: Balanced composition (true/false)

4. DEPTH: shallow (bokeh/blur), moderate, deep (everything in focus)

5. FOCAL POINT: What draws the eye

6. SUBJECT PLACEMENT: center, left-third, right-third, top, bottom

7. ELEMENTS:
   - backgroundElements: List key background items
   - foregroundElements: List key foreground items

8. VISUAL PROPERTIES:
   - colorPalette: Dominant colors (e.g., ["blue", "orange"])
   - contrast: low, medium, high
   - balance: balanced, unbalanced, asymmetric

Format response as JSON:
{
  "shotSize": "close_up|medium_shot|wide_shot|etc",
  "shotAngle": "eye_level|high_angle|low_angle|birds_eye|dutch",
  "ruleOfThirds": true|false,
  "leadingLines": true|false,
  "symmetry": true|false,
  "depth": "shallow|moderate|deep",
  "focalPoint": "description",
  "subjectPlacement": "center|left-third|right-third|etc",
  "backgroundElements": ["item1", "item2"],
  "foregroundElements": ["item1", "item2"],
  "colorPalette": ["color1", "color2"],
  "contrast": "low|medium|high",
  "balance": "balanced|unbalanced|asymmetric",
  "confidence": 0.0-1.0
}`

	visionReq := models.MageAgentVisionRequest{
		Image:     frameData,
		Prompt:    prompt,
		MaxTokens: 1000,
	}

	visionResp, err := sca.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Parse response
	composition, err := sca.parseCompositionResponse(visionResp.Description)
	if err != nil {
		// Fallback to heuristic analysis
		return sca.fallbackComposition(visionResp.Description), nil
	}

	composition.Timestamp = time.Now()
	return composition, nil
}

// parseCompositionResponse parses AI response into ShotComposition
func (sca *ShotCompositionAnalyzer) parseCompositionResponse(response string) (*ShotComposition, error) {
	jsonStr := sca.extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result struct {
		ShotSize           string                 `json:"shotSize"`
		ShotAngle          string                 `json:"shotAngle"`
		RuleOfThirds       bool                   `json:"ruleOfThirds"`
		LeadingLines       bool                   `json:"leadingLines"`
		Symmetry           bool                   `json:"symmetry"`
		Depth              string                 `json:"depth"`
		FocalPoint         string                 `json:"focalPoint"`
		SubjectPlacement   string                 `json:"subjectPlacement"`
		BackgroundElements []string               `json:"backgroundElements"`
		ForegroundElements []string               `json:"foregroundElements"`
		ColorPalette       []string               `json:"colorPalette"`
		Contrast           string                 `json:"contrast"`
		Balance            string                 `json:"balance"`
		Confidence         float64                `json:"confidence"`
		Attributes         map[string]interface{} `json:"attributes"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	composition := &ShotComposition{
		ShotSize:           ShotSize(result.ShotSize),
		ShotAngle:          ShotAngle(result.ShotAngle),
		RuleOfThirds:       result.RuleOfThirds,
		LeadingLines:       result.LeadingLines,
		Symmetry:           result.Symmetry,
		Depth:              result.Depth,
		FocalPoint:         result.FocalPoint,
		SubjectPlacement:   result.SubjectPlacement,
		BackgroundElements: result.BackgroundElements,
		ForegroundElements: result.ForegroundElements,
		ColorPalette:       result.ColorPalette,
		Contrast:           result.Contrast,
		Balance:            result.Balance,
		Confidence:         result.Confidence,
		Attributes:         result.Attributes,
	}

	// Validate and normalize
	if !sca.isValidShotSize(composition.ShotSize) {
		composition.ShotSize = ShotSizeUnknown
		composition.Confidence *= 0.7
	}

	if !sca.isValidShotAngle(composition.ShotAngle) {
		composition.ShotAngle = ShotAngleUnknown
		composition.Confidence *= 0.7
	}

	return composition, nil
}

// extractJSON extracts JSON from text
func (sca *ShotCompositionAnalyzer) extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")

	if start == -1 || end == -1 || end <= start {
		return ""
	}

	return text[start : end+1]
}

// isValidShotSize checks if shot size is valid
func (sca *ShotCompositionAnalyzer) isValidShotSize(size ShotSize) bool {
	validSizes := []ShotSize{
		ShotSizeExtremeCloseUp,
		ShotSizeCloseUp,
		ShotSizeMediumCloseUp,
		ShotSizeMediumShot,
		ShotSizeMediumWide,
		ShotSizeWideShot,
		ShotSizeExtremeWide,
	}

	for _, valid := range validSizes {
		if size == valid {
			return true
		}
	}
	return false
}

// isValidShotAngle checks if shot angle is valid
func (sca *ShotCompositionAnalyzer) isValidShotAngle(angle ShotAngle) bool {
	validAngles := []ShotAngle{
		ShotAngleEyeLevel,
		ShotAngleHighAngle,
		ShotAngleLowAngle,
		ShotAngleBirdsEye,
		ShotAngleDutch,
	}

	for _, valid := range validAngles {
		if angle == valid {
			return true
		}
	}
	return false
}

// fallbackComposition provides heuristic composition when AI parsing fails
func (sca *ShotCompositionAnalyzer) fallbackComposition(analysis string) *ShotComposition {
	composition := &ShotComposition{
		ShotSize:           ShotSizeUnknown,
		ShotAngle:          ShotAngleUnknown,
		Confidence:         0.3,
		BackgroundElements: []string{},
		ForegroundElements: []string{},
		ColorPalette:       []string{},
		Attributes:         make(map[string]interface{}),
		Timestamp:          time.Now(),
	}

	lowerAnalysis := strings.ToLower(analysis)

	// Detect shot size keywords
	if strings.Contains(lowerAnalysis, "close") || strings.Contains(lowerAnalysis, "detail") {
		composition.ShotSize = ShotSizeCloseUp
		composition.Confidence = 0.5
	} else if strings.Contains(lowerAnalysis, "wide") || strings.Contains(lowerAnalysis, "landscape") {
		composition.ShotSize = ShotSizeWideShot
		composition.Confidence = 0.5
	} else if strings.Contains(lowerAnalysis, "medium") || strings.Contains(lowerAnalysis, "person") {
		composition.ShotSize = ShotSizeMediumShot
		composition.Confidence = 0.5
	}

	// Detect angle keywords
	if strings.Contains(lowerAnalysis, "above") || strings.Contains(lowerAnalysis, "looking down") {
		composition.ShotAngle = ShotAngleHighAngle
	} else if strings.Contains(lowerAnalysis, "below") || strings.Contains(lowerAnalysis, "looking up") {
		composition.ShotAngle = ShotAngleLowAngle
	} else {
		composition.ShotAngle = ShotAngleEyeLevel
	}

	return composition
}

// GetCompositionStatistics computes statistics for composition analyses
func (sca *ShotCompositionAnalyzer) GetCompositionStatistics(compositions []*ShotComposition) map[string]interface{} {
	stats := make(map[string]interface{})

	shotSizeCounts := make(map[ShotSize]int)
	shotAngleCounts := make(map[ShotAngle]int)
	ruleOfThirdsCount := 0
	leadingLinesCount := 0
	symmetryCount := 0

	totalConfidence := 0.0

	for _, comp := range compositions {
		shotSizeCounts[comp.ShotSize]++
		shotAngleCounts[comp.ShotAngle]++

		if comp.RuleOfThirds {
			ruleOfThirdsCount++
		}
		if comp.LeadingLines {
			leadingLinesCount++
		}
		if comp.Symmetry {
			symmetryCount++
		}

		totalConfidence += comp.Confidence
	}

	stats["totalShots"] = len(compositions)
	stats["shotSizes"] = shotSizeCounts
	stats["shotAngles"] = shotAngleCounts
	stats["ruleOfThirdsUsage"] = float64(ruleOfThirdsCount) / float64(len(compositions))
	stats["leadingLinesUsage"] = float64(leadingLinesCount) / float64(len(compositions))
	stats["symmetryUsage"] = float64(symmetryCount) / float64(len(compositions))
	stats["averageConfidence"] = totalConfidence / float64(len(compositions))

	return stats
}

// DetectShotChanges identifies significant composition changes
func (sca *ShotCompositionAnalyzer) DetectShotChanges(compositions []*ShotComposition) []int {
	if len(compositions) < 2 {
		return []int{}
	}

	changes := []int{}

	for i := 1; i < len(compositions); i++ {
		prev := compositions[i-1]
		curr := compositions[i]

		// Detect shot size change
		if prev.ShotSize != curr.ShotSize && curr.ShotSize != ShotSizeUnknown {
			changes = append(changes, i)
			continue
		}

		// Detect angle change
		if prev.ShotAngle != curr.ShotAngle && curr.ShotAngle != ShotAngleUnknown {
			changes = append(changes, i)
		}
	}

	return changes
}
