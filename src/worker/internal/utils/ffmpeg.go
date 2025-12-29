package utils

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FFmpegHelper provides utilities for FFmpeg operations
type FFmpegHelper struct {
	ffmpegPath  string
	ffprobePath string
	tempDir     string
}

// NewFFmpegHelper creates a new FFmpeg helper
func NewFFmpegHelper(tempDir string) (*FFmpegHelper, error) {
	// Verify FFmpeg installation
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found in PATH: %w", err)
	}

	// Ensure temp directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &FFmpegHelper{
		ffmpegPath:  ffmpegPath,
		ffprobePath: ffprobePath,
		tempDir:     tempDir,
	}, nil
}

// SaveVideoFromBuffer saves base64-encoded video data to temp file
func (h *FFmpegHelper) SaveVideoFromBuffer(videoBuffer []byte, jobID string) (string, error) {
	// Decode base64 if needed
	var videoData []byte

	// Try to decode as base64
	if decoded, decodeErr := base64.StdEncoding.DecodeString(string(videoBuffer)); decodeErr == nil {
		videoData = decoded
	} else {
		// Already raw bytes
		videoData = videoBuffer
	}

	// Create temp file
	tempFile := filepath.Join(h.tempDir, fmt.Sprintf("%s_input.mp4", jobID))
	if err := os.WriteFile(tempFile, videoData, 0644); err != nil {
		return "", fmt.Errorf("failed to write video file: %w", err)
	}

	return tempFile, nil
}

// GetVideoMetadata extracts video metadata using ffprobe
func (h *FFmpegHelper) GetVideoMetadata(videoPath string) (map[string]interface{}, error) {
	cmd := exec.Command(h.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	// Parse JSON output from ffprobe
	var ffprobeData struct{
		Streams []struct {
			CodecType  string  `json:"codec_type"`
			CodecName  string  `json:"codec_name"`
			Width      int     `json:"width"`
			Height     int     `json:"height"`
			RFrameRate string  `json:"r_frame_rate"`
			Duration   string  `json:"duration"`
			BitRate    string  `json:"bit_rate"`
		} `json:"streams"`
		Format struct {
			Duration   string `json:"duration"`
			Size       string `json:"size"`
			BitRate    string `json:"bit_rate"`
			FormatName string `json:"format_name"`
		} `json:"format"`
	}

	if err := json.Unmarshal(output, &ffprobeData); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe JSON: %w", err)
	}

	// Build metadata map with all available information
	metadata := make(map[string]interface{})

	// Format-level metadata
	if ffprobeData.Format.Duration != "" {
		if duration, err := strconv.ParseFloat(ffprobeData.Format.Duration, 64); err == nil {
			metadata["duration"] = duration
		}
	}
	if ffprobeData.Format.Size != "" {
		if size, err := strconv.ParseInt(ffprobeData.Format.Size, 10, 64); err == nil {
			metadata["size"] = size
		}
	}
	if ffprobeData.Format.BitRate != "" {
		if bitrate, err := strconv.ParseInt(ffprobeData.Format.BitRate, 10, 64); err == nil {
			metadata["bitrate"] = bitrate
		}
	}
	metadata["format"] = ffprobeData.Format.FormatName

	// Stream-level metadata (prioritize video stream)
	for _, stream := range ffprobeData.Streams {
		if stream.CodecType == "video" {
			metadata["width"] = stream.Width
			metadata["height"] = stream.Height
			metadata["codec"] = stream.CodecName

			// Parse frame rate (format: "30/1" or "30000/1001")
			if stream.RFrameRate != "" {
				parts := strings.Split(stream.RFrameRate, "/")
				if len(parts) == 2 {
					if num, err1 := strconv.ParseFloat(parts[0], 64); err1 == nil {
						if den, err2 := strconv.ParseFloat(parts[1], 64); err2 == nil && den > 0 {
							metadata["fps"] = num / den
						}
					}
				}
			}

			// Video stream duration (if available)
			if stream.Duration != "" {
				if dur, err := strconv.ParseFloat(stream.Duration, 64); err == nil {
					// Only override if format duration not available
					if _, exists := metadata["duration"]; !exists {
						metadata["duration"] = dur
					}
				}
			}

			// Video stream bitrate (if available)
			if stream.BitRate != "" {
				if br, err := strconv.ParseInt(stream.BitRate, 10, 64); err == nil {
					// Only override if format bitrate not available
					if _, exists := metadata["bitrate"]; !exists {
						metadata["bitrate"] = br
					}
				}
			}

			// Found video stream, stop looking
			break
		}
	}

	// Audio codec (find first audio stream)
	for _, stream := range ffprobeData.Streams {
		if stream.CodecType == "audio" {
			metadata["audio_codec"] = stream.CodecName
			break
		}
	}

	return metadata, nil
}

// ExtractFrame extracts a single frame at specified timestamp
func (h *FFmpegHelper) ExtractFrame(videoPath string, timestamp float64, outputPath string) error {
	cmd := exec.Command(h.ffmpegPath,
		"-ss", fmt.Sprintf("%.2f", timestamp),
		"-i", videoPath,
		"-vframes", "1",
		"-q:v", "2", // High quality
		"-y", // Overwrite
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("frame extraction failed: %w", err)
	}

	return nil
}

// ExtractFrames extracts multiple frames based on sampling mode
func (h *FFmpegHelper) ExtractFrames(videoPath string, mode string, rate int, maxFrames int, duration float64, outputDir string) ([]string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	switch mode {
	case "keyframes":
		// Extract only keyframes (I-frames)
		framePaths, err := h.extractKeyframes(videoPath, maxFrames, outputDir)
		if err != nil {
			return nil, err
		}
		return framePaths, nil

	case "uniform":
		// Extract frames at uniform intervals
		framePaths, err := h.extractUniformFrames(videoPath, rate, maxFrames, duration, outputDir)
		if err != nil {
			return nil, err
		}
		return framePaths, nil

	case "scene-based":
		// Extract frames at scene changes
		framePaths, err := h.extractSceneFrames(videoPath, maxFrames, outputDir)
		if err != nil {
			return nil, err
		}
		return framePaths, nil

	default:
		return nil, fmt.Errorf("unknown sampling mode: %s", mode)
	}
}

// extractKeyframes extracts I-frames (keyframes) from video
func (h *FFmpegHelper) extractKeyframes(videoPath string, maxFrames int, outputDir string) ([]string, error) {
	outputPattern := filepath.Join(outputDir, "frame_%04d.jpg")

	cmd := exec.Command(h.ffmpegPath,
		"-i", videoPath,
		"-vf", "select='eq(pict_type\\,I)'", // Select I-frames only
		"-vsync", "vfr",
		"-q:v", "2",
		"-frames:v", strconv.Itoa(maxFrames),
		"-y",
		outputPattern,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("keyframe extraction failed: %w", err)
	}

	return h.collectFramePaths(outputDir), nil
}

// extractUniformFrames extracts frames at uniform time intervals
func (h *FFmpegHelper) extractUniformFrames(videoPath string, fps int, maxFrames int, duration float64, outputDir string) ([]string, error) {
	outputPattern := filepath.Join(outputDir, "frame_%04d.jpg")

	// Calculate frame rate to extract
	var vf string
	if fps > 0 {
		vf = fmt.Sprintf("fps=%d", fps)
	} else {
		// Calculate fps based on maxFrames and duration
		targetFPS := float64(maxFrames) / duration
		vf = fmt.Sprintf("fps=%.2f", targetFPS)
	}

	cmd := exec.Command(h.ffmpegPath,
		"-i", videoPath,
		"-vf", vf,
		"-q:v", "2",
		"-frames:v", strconv.Itoa(maxFrames),
		"-y",
		outputPattern,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("uniform frame extraction failed: %w", err)
	}

	return h.collectFramePaths(outputDir), nil
}

// extractSceneFrames extracts frames at scene changes
func (h *FFmpegHelper) extractSceneFrames(videoPath string, maxFrames int, outputDir string) ([]string, error) {
	outputPattern := filepath.Join(outputDir, "frame_%04d.jpg")

	// Use scene detection filter
	cmd := exec.Command(h.ffmpegPath,
		"-i", videoPath,
		"-vf", "select='gt(scene\\,0.3)',showinfo", // Scene change threshold 0.3
		"-vsync", "vfr",
		"-q:v", "2",
		"-frames:v", strconv.Itoa(maxFrames),
		"-y",
		outputPattern,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scene-based extraction failed: %w", err)
	}

	return h.collectFramePaths(outputDir), nil
}

// collectFramePaths collects all frame paths from output directory
func (h *FFmpegHelper) collectFramePaths(outputDir string) []string {
	files, err := os.ReadDir(outputDir)
	if err != nil {
		return []string{}
	}

	var framePaths []string
	for _, file := range files {
		if !file.IsDir() && (strings.HasSuffix(file.Name(), ".jpg") || strings.HasSuffix(file.Name(), ".png")) {
			framePaths = append(framePaths, filepath.Join(outputDir, file.Name()))
		}
	}

	return framePaths
}

// ExtractAudio extracts audio track to WAV format
func (h *FFmpegHelper) ExtractAudio(videoPath string, outputPath string) error {
	cmd := exec.Command(h.ffmpegPath,
		"-i", videoPath,
		"-vn", // No video
		"-acodec", "pcm_s16le", // PCM WAV format
		"-ar", "16000", // 16kHz sample rate (optimal for speech recognition)
		"-ac", "1", // Mono
		"-y",
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("audio extraction failed: %w", err)
	}

	return nil
}

// GetVideoDuration returns video duration in seconds
func (h *FFmpegHelper) GetVideoDuration(videoPath string) (float64, error) {
	cmd := exec.Command(h.ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get duration: %w", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

// GetFrameRate returns video frame rate
func (h *FFmpegHelper) GetFrameRate(videoPath string) (float64, error) {
	cmd := exec.Command(h.ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get frame rate: %w", err)
	}

	rateStr := strings.TrimSpace(string(output))

	// Parse fraction (e.g., "30000/1001")
	parts := strings.Split(rateStr, "/")
	if len(parts) == 2 {
		num, _ := strconv.ParseFloat(parts[0], 64)
		den, _ := strconv.ParseFloat(parts[1], 64)
		if den > 0 {
			return num / den, nil
		}
	}

	// Try direct parse
	rate, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse frame rate: %w", err)
	}

	return rate, nil
}

// GetResolution returns video resolution (width, height)
func (h *FFmpegHelper) GetResolution(videoPath string) (int, int, error) {
	cmd := exec.Command(h.ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get resolution: %w", err)
	}

	resStr := strings.TrimSpace(string(output))
	parts := strings.Split(resStr, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid resolution format: %s", resStr)
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse width: %w", err)
	}

	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse height: %w", err)
	}

	return width, height, nil
}

// EncodeFrameToBase64 reads a frame and encodes it to base64
func (h *FFmpegHelper) EncodeFrameToBase64(framePath string) (string, error) {
	data, err := os.ReadFile(framePath)
	if err != nil {
		return "", fmt.Errorf("failed to read frame: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return encoded, nil
}

// EncodeAudioToBase64 reads an audio file and encodes it to base64
func (h *FFmpegHelper) EncodeAudioToBase64(audioPath string) (string, error) {
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return encoded, nil
}

// Cleanup removes temporary files
func (h *FFmpegHelper) Cleanup(paths ...string) error {
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to cleanup %s: %w", path, err)
		}
	}
	return nil
}

// GetAudioFileSize returns the size of an audio file in bytes
func (h *FFmpegHelper) GetAudioFileSize(audioPath string) (int64, error) {
	fileInfo, err := os.Stat(audioPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get audio file size: %w", err)
	}
	return fileInfo.Size(), nil
}

// ChunkAudio splits large audio files into smaller chunks for processing
// Returns paths to audio chunk files
func (h *FFmpegHelper) ChunkAudio(audioPath string, chunkSizeMB int) ([]string, error) {
	// Get audio duration to calculate chunk duration
	duration, err := h.GetAudioDuration(audioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get audio duration: %w", err)
	}

	// Get file size to estimate bitrate
	fileInfo, err := os.Stat(audioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Calculate bitrate (bytes per second)
	bitrate := float64(fileInfo.Size()) / duration

	// Calculate chunk duration for target chunk size
	chunkSizeBytes := int64(chunkSizeMB * 1024 * 1024)
	chunkDuration := float64(chunkSizeBytes) / bitrate

	// Generate chunks with 2-second overlap for seamless transcription
	var chunkPaths []string
	overlap := 2.0 // seconds
	currentStart := 0.0

	chunkIndex := 0
	for currentStart < duration {
		chunkEnd := currentStart + chunkDuration
		if chunkEnd > duration {
			chunkEnd = duration
		}

		// Output path for this chunk
		chunkPath := filepath.Join(h.tempDir, fmt.Sprintf("audio_chunk_%03d.wav", chunkIndex))

		// Extract chunk with FFmpeg
		cmd := exec.Command(h.ffmpegPath,
			"-ss", fmt.Sprintf("%.2f", currentStart),
			"-t", fmt.Sprintf("%.2f", chunkEnd-currentStart),
			"-i", audioPath,
			"-acodec", "pcm_s16le",
			"-ar", "16000",
			"-ac", "1",
			"-y",
			chunkPath,
		)

		if err := cmd.Run(); err != nil {
			// Cleanup on error
			h.Cleanup(chunkPaths...)
			return nil, fmt.Errorf("failed to create audio chunk %d: %w", chunkIndex, err)
		}

		chunkPaths = append(chunkPaths, chunkPath)

		// Move to next chunk (with overlap)
		currentStart += chunkDuration - overlap
		chunkIndex++
	}

	return chunkPaths, nil
}

// GetAudioDuration returns audio duration in seconds
func (h *FFmpegHelper) GetAudioDuration(audioPath string) (float64, error) {
	cmd := exec.Command(h.ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		audioPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get audio duration: %w", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse audio duration: %w", err)
	}

	return duration, nil
}

// ValidateVideo checks if video file is valid
func (h *FFmpegHelper) ValidateVideo(videoPath string) error {
	cmd := exec.Command(h.ffprobePath,
		"-v", "error",
		videoPath,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("invalid video file: %w", err)
	}

	return nil
}
