package scene

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
)

// ColorGrading represents color grading style
type ColorGrading string

const (
	GradingNatural     ColorGrading = "natural"
	GradingWarm        ColorGrading = "warm"
	GradingCool        ColorGrading = "cool"
	GradingDesaturated ColorGrading = "desaturated"
	GradingVibrant     ColorGrading = "vibrant"
	GradingHighContrast ColorGrading = "high_contrast"
	GradingLowContrast  ColorGrading = "low_contrast"
	GradingMonochrome  ColorGrading = "monochrome"
	GradingSepia       ColorGrading = "sepia"
	GradingCinematic   ColorGrading = "cinematic"
)

// LightingSetup represents lighting configuration
type LightingSetup string

const (
	LightingThreePoint    LightingSetup = "three_point"
	LightingNatural       LightingSetup = "natural"
	LightingPractical     LightingSetup = "practical"
	LightingHardLight     LightingSetup = "hard_light"
	LightingSoftLight     LightingSetup = "soft_light"
	LightingBacklight     LightingSetup = "backlight"
	LightingRimLight      LightingSetup = "rim_light"
	LightingSideLight     LightingSetup = "side_light"
	LightingLowKey        LightingSetup = "low_key"
	LightingHighKey       LightingSetup = "high_key"
	LightingSilhouette    LightingSetup = "silhouette"
	LightingChiaroscuro   LightingSetup = "chiaroscuro"
)

// MoodType represents emotional mood
type MoodType string

const (
	MoodTense      MoodType = "tense"
	MoodPeaceful   MoodType = "peaceful"
	MoodEnergetic  MoodType = "energetic"
	MoodMelancholy MoodType = "melancholy"
	MoodHopeful    MoodType = "hopeful"
	MoodOminous    MoodType = "ominous"
	MoodJoyful     MoodType = "joyful"
	MoodMysterious MoodType = "mysterious"
	MoodRomantic   MoodType = "romantic"
	MoodNostalgic  MoodType = "nostalgic"
	MoodSurreal    MoodType = "surreal"
	MoodNeutral    MoodType = "neutral"
)

// ColorTemperature represents color temperature
type ColorTemperature string

const (
	TempVeryWarm  ColorTemperature = "very_warm"
	TempWarm      ColorTemperature = "warm"
	TempNeutral   ColorTemperature = "neutral"
	TempCool      ColorTemperature = "cool"
	TempVeryCool  ColorTemperature = "very_cool"
)

// ColorLightingMood represents comprehensive visual analysis
type ColorLightingMood struct {
	// Color Analysis
	ColorGrading      ColorGrading       `json:"colorGrading"`
	ColorTemperature  ColorTemperature   `json:"colorTemperature"`
	DominantColors    []string           `json:"dominantColors"`
	ColorPalette      []string           `json:"colorPalette"`
	Saturation        SaturationAnalysis `json:"saturation"`
	Contrast          ContrastAnalysis   `json:"contrast"`

	// Lighting Analysis
	LightingSetup     LightingSetup       `json:"lightingSetup"`
	LightingDirection string              `json:"lightingDirection"` // front, side, back, top, bottom, mixed
	LightingQuality   string              `json:"lightingQuality"`   // hard, soft, mixed
	LightSources      []LightSource       `json:"lightSources"`
	Shadows           ShadowAnalysis      `json:"shadows"`
	Highlights        HighlightAnalysis   `json:"highlights"`
	DynamicRange      string              `json:"dynamicRange"` // low, medium, high, very_high

	// Mood Analysis
	PrimaryMood       MoodType               `json:"primaryMood"`
	SecondaryMood     *MoodType              `json:"secondaryMood,omitempty"`
	MoodIntensity     float64                `json:"moodIntensity"` // 0-1
	EmotionalTone     string                 `json:"emotionalTone"`
	Atmosphere        string                 `json:"atmosphere"`

	// Technical Metrics
	Confidence        float64                `json:"confidence"`
	Attributes        map[string]interface{} `json:"attributes"`
	Timestamp         time.Time              `json:"timestamp"`
}

// SaturationAnalysis represents color saturation analysis
type SaturationAnalysis struct {
	Level       string  `json:"level"`       // very_low, low, medium, high, very_high
	Value       float64 `json:"value"`       // 0-1
	Uniformity  string  `json:"uniformity"`  // uniform, varied, mixed
	Description string  `json:"description"` // e.g., "Vibrant colors with high saturation"
}

// ContrastAnalysis represents contrast analysis
type ContrastAnalysis struct {
	Level       string  `json:"level"`       // very_low, low, medium, high, very_high
	Value       float64 `json:"value"`       // 0-1
	Distribution string  `json:"distribution"` // even, centered, edges, mixed
	Description string  `json:"description"`
}

// LightSource represents a detected light source
type LightSource struct {
	Type        string  `json:"type"`        // key, fill, back, practical, natural
	Position    string  `json:"position"`    // front_left, top_right, etc.
	Intensity   string  `json:"intensity"`   // low, medium, high
	Temperature string  `json:"temperature"` // warm, neutral, cool
	Hardness    string  `json:"hardness"`    // soft, medium, hard
}

// ShadowAnalysis represents shadow characteristics
type ShadowAnalysis struct {
	Presence    string  `json:"presence"`    // none, minimal, moderate, prominent, dominant
	Hardness    string  `json:"hardness"`    // soft, medium, hard
	Coverage    float64 `json:"coverage"`    // 0-1 (percentage of frame)
	Direction   string  `json:"direction"`   // Direction shadows are cast
	Depth       string  `json:"depth"`       // shallow, medium, deep
	Description string  `json:"description"`
}

// HighlightAnalysis represents highlight characteristics
type HighlightAnalysis struct {
	Presence     string  `json:"presence"`     // none, minimal, moderate, prominent, blown_out
	Coverage     float64 `json:"coverage"`     // 0-1
	Clipping     bool    `json:"clipping"`     // Whether highlights are clipped
	Specular     bool    `json:"specular"`     // Specular highlights present
	Distribution string  `json:"distribution"` // localized, distributed, uniform
	Description  string  `json:"description"`
}

// ColorLightingMoodAnalyzer analyzes color, lighting, and mood
type ColorLightingMoodAnalyzer struct {
	mageAgent *clients.MageAgentClient
}

// NewColorLightingMoodAnalyzer creates a new analyzer
func NewColorLightingMoodAnalyzer(mageAgent *clients.MageAgentClient) *ColorLightingMoodAnalyzer {
	return &ColorLightingMoodAnalyzer{
		mageAgent: mageAgent,
	}
}

// Analyze performs comprehensive color, lighting, and mood analysis
func (clma *ColorLightingMoodAnalyzer) Analyze(ctx context.Context, frameData string) (*ColorLightingMood, error) {
	startTime := time.Now()

	// Prepare vision request with comprehensive prompt
	prompt := `Analyze this video frame's color, lighting, and mood with comprehensive detail:

1. COLOR GRADING: Choose from natural, warm, cool, desaturated, vibrant, high_contrast, low_contrast, monochrome, sepia, cinematic

2. COLOR TEMPERATURE: very_warm, warm, neutral, cool, very_cool

3. DOMINANT COLORS: List 3-5 primary colors (e.g., ["deep blue", "golden yellow", "crimson red"])

4. COLOR PALETTE: List 6-10 colors that define the frame (hex codes or names)

5. SATURATION:
   - Level: very_low, low, medium, high, very_high
   - Value: 0.0-1.0
   - Uniformity: uniform, varied, mixed
   - Description: Brief description

6. CONTRAST:
   - Level: very_low, low, medium, high, very_high
   - Value: 0.0-1.0
   - Distribution: even, centered, edges, mixed
   - Description: Brief description

7. LIGHTING SETUP: three_point, natural, practical, hard_light, soft_light, backlight, rim_light, side_light, low_key, high_key, silhouette, chiaroscuro

8. LIGHTING DETAILS:
   - Direction: front, side, back, top, bottom, mixed
   - Quality: hard, soft, mixed
   - Dynamic Range: low, medium, high, very_high

9. LIGHT SOURCES: For each detectable source:
   - Type: key, fill, back, practical, natural
   - Position: front_left, top_right, etc.
   - Intensity: low, medium, high
   - Temperature: warm, neutral, cool
   - Hardness: soft, medium, hard

10. SHADOWS:
    - Presence: none, minimal, moderate, prominent, dominant
    - Hardness: soft, medium, hard
    - Coverage: 0.0-1.0
    - Direction: where shadows point
    - Depth: shallow, medium, deep
    - Description: characteristics

11. HIGHLIGHTS:
    - Presence: none, minimal, moderate, prominent, blown_out
    - Coverage: 0.0-1.0
    - Clipping: yes/no
    - Specular: yes/no (shiny highlights)
    - Distribution: localized, distributed, uniform
    - Description: characteristics

12. MOOD:
    - Primary Mood: tense, peaceful, energetic, melancholy, hopeful, ominous, joyful, mysterious, romantic, nostalgic, surreal, neutral
    - Secondary Mood (optional): secondary emotional tone
    - Intensity: 0.0-1.0
    - Emotional Tone: overall emotional feeling
    - Atmosphere: descriptive atmosphere

Respond with JSON format:
{
  "colorGrading": "natural|warm|cool|...",
  "colorTemperature": "very_warm|warm|neutral|cool|very_cool",
  "dominantColors": ["color1", "color2", "color3"],
  "colorPalette": ["#hex1", "#hex2", ...],
  "saturation": {
    "level": "very_low|low|medium|high|very_high",
    "value": 0.0-1.0,
    "uniformity": "uniform|varied|mixed",
    "description": "..."
  },
  "contrast": {
    "level": "very_low|low|medium|high|very_high",
    "value": 0.0-1.0,
    "distribution": "even|centered|edges|mixed",
    "description": "..."
  },
  "lightingSetup": "three_point|natural|...",
  "lightingDirection": "front|side|back|top|bottom|mixed",
  "lightingQuality": "hard|soft|mixed",
  "dynamicRange": "low|medium|high|very_high",
  "lightSources": [
    {
      "type": "key|fill|back|practical|natural",
      "position": "front_left|...",
      "intensity": "low|medium|high",
      "temperature": "warm|neutral|cool",
      "hardness": "soft|medium|hard"
    }
  ],
  "shadows": {
    "presence": "none|minimal|moderate|prominent|dominant",
    "hardness": "soft|medium|hard",
    "coverage": 0.0-1.0,
    "direction": "...",
    "depth": "shallow|medium|deep",
    "description": "..."
  },
  "highlights": {
    "presence": "none|minimal|moderate|prominent|blown_out",
    "coverage": 0.0-1.0,
    "clipping": true|false,
    "specular": true|false,
    "distribution": "localized|distributed|uniform",
    "description": "..."
  },
  "primaryMood": "tense|peaceful|energetic|...",
  "secondaryMood": "optional",
  "moodIntensity": 0.0-1.0,
  "emotionalTone": "...",
  "atmosphere": "...",
  "confidence": 0.0-1.0,
  "attributes": {
    "notes": "any additional observations"
  }
}`

	visionReq := models.MageAgentVisionRequest{
		Image:     frameData,
		Prompt:    prompt,
		MaxTokens: 1500,
	}

	visionResp, err := clma.mageAgent.AnalyzeFrame(ctx, visionReq)
	if err != nil {
		return nil, fmt.Errorf("vision analysis failed: %w", err)
	}

	// Parse JSON response
	analysis, err := clma.parseAnalysisResponse(visionResp.Description)
	if err != nil {
		log.Printf("Failed to parse analysis response, using fallback: %v", err)
		analysis = clma.fallbackAnalysis(frameData)
	}

	analysis.Timestamp = startTime

	log.Printf("Analyzed color/lighting/mood: grading=%s, lighting=%s, mood=%s (confidence: %.2f)",
		analysis.ColorGrading, analysis.LightingSetup, analysis.PrimaryMood, analysis.Confidence)

	return analysis, nil
}

// AnalyzeBatch analyzes multiple frames concurrently
func (clma *ColorLightingMoodAnalyzer) AnalyzeBatch(ctx context.Context, frames []string) ([]*ColorLightingMood, error) {
	results := make([]*ColorLightingMood, len(frames))
	errChan := make(chan error, len(frames))

	for i, frame := range frames {
		go func(idx int, frameData string) {
			analysis, err := clma.Analyze(ctx, frameData)
			if err != nil {
				errChan <- fmt.Errorf("frame %d: %w", idx, err)
				return
			}
			results[idx] = analysis
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

// parseAnalysisResponse parses the AI response into ColorLightingMood
func (clma *ColorLightingMoodAnalyzer) parseAnalysisResponse(response string) (*ColorLightingMood, error) {
	// Try to extract JSON from markdown code blocks
	jsonStr := extractJSON(response)

	var rawAnalysis struct {
		ColorGrading      string              `json:"colorGrading"`
		ColorTemperature  string              `json:"colorTemperature"`
		DominantColors    []string            `json:"dominantColors"`
		ColorPalette      []string            `json:"colorPalette"`
		Saturation        SaturationAnalysis  `json:"saturation"`
		Contrast          ContrastAnalysis    `json:"contrast"`
		LightingSetup     string              `json:"lightingSetup"`
		LightingDirection string              `json:"lightingDirection"`
		LightingQuality   string              `json:"lightingQuality"`
		DynamicRange      string              `json:"dynamicRange"`
		LightSources      []LightSource       `json:"lightSources"`
		Shadows           ShadowAnalysis      `json:"shadows"`
		Highlights        HighlightAnalysis   `json:"highlights"`
		PrimaryMood       string              `json:"primaryMood"`
		SecondaryMood     string              `json:"secondaryMood"`
		MoodIntensity     float64             `json:"moodIntensity"`
		EmotionalTone     string              `json:"emotionalTone"`
		Atmosphere        string              `json:"atmosphere"`
		Confidence        float64             `json:"confidence"`
		Attributes        map[string]interface{} `json:"attributes"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawAnalysis); err != nil {
		return nil, fmt.Errorf("failed to unmarshal analysis JSON: %w", err)
	}

	analysis := &ColorLightingMood{
		ColorGrading:      ColorGrading(rawAnalysis.ColorGrading),
		ColorTemperature:  ColorTemperature(rawAnalysis.ColorTemperature),
		DominantColors:    rawAnalysis.DominantColors,
		ColorPalette:      rawAnalysis.ColorPalette,
		Saturation:        rawAnalysis.Saturation,
		Contrast:          rawAnalysis.Contrast,
		LightingSetup:     LightingSetup(rawAnalysis.LightingSetup),
		LightingDirection: rawAnalysis.LightingDirection,
		LightingQuality:   rawAnalysis.LightingQuality,
		DynamicRange:      rawAnalysis.DynamicRange,
		LightSources:      rawAnalysis.LightSources,
		Shadows:           rawAnalysis.Shadows,
		Highlights:        rawAnalysis.Highlights,
		PrimaryMood:       MoodType(rawAnalysis.PrimaryMood),
		MoodIntensity:     rawAnalysis.MoodIntensity,
		EmotionalTone:     rawAnalysis.EmotionalTone,
		Atmosphere:        rawAnalysis.Atmosphere,
		Confidence:        rawAnalysis.Confidence,
		Attributes:        rawAnalysis.Attributes,
	}

	if rawAnalysis.SecondaryMood != "" {
		secondaryMood := MoodType(rawAnalysis.SecondaryMood)
		analysis.SecondaryMood = &secondaryMood
	}

	return analysis, nil
}

// fallbackAnalysis provides basic analysis when AI parsing fails
func (clma *ColorLightingMoodAnalyzer) fallbackAnalysis(frameData string) *ColorLightingMood {
	return &ColorLightingMood{
		ColorGrading:      GradingNatural,
		ColorTemperature:  TempNeutral,
		DominantColors:    []string{"unknown"},
		ColorPalette:      []string{"#808080"},
		Saturation: SaturationAnalysis{
			Level:       "medium",
			Value:       0.5,
			Uniformity:  "uniform",
			Description: "Unable to analyze",
		},
		Contrast: ContrastAnalysis{
			Level:        "medium",
			Value:        0.5,
			Distribution: "even",
			Description:  "Unable to analyze",
		},
		LightingSetup:     LightingNatural,
		LightingDirection: "mixed",
		LightingQuality:   "mixed",
		DynamicRange:      "medium",
		LightSources:      []LightSource{},
		Shadows: ShadowAnalysis{
			Presence:    "moderate",
			Hardness:    "medium",
			Coverage:    0.3,
			Direction:   "unknown",
			Depth:       "medium",
			Description: "Unable to analyze",
		},
		Highlights: HighlightAnalysis{
			Presence:     "moderate",
			Coverage:     0.2,
			Clipping:     false,
			Specular:     false,
			Distribution: "distributed",
			Description:  "Unable to analyze",
		},
		PrimaryMood:   MoodNeutral,
		MoodIntensity: 0.5,
		EmotionalTone: "neutral",
		Atmosphere:    "unknown",
		Confidence:    0.3,
		Attributes: map[string]interface{}{
			"fallback": true,
			"reason":   "AI parsing failed",
		},
	}
}
