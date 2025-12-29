package extractor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/adverant/nexus/videoagent-worker/internal/models"
	"github.com/adverant/nexus/videoagent-worker/internal/utils"
)

// FFprobeOutput represents the JSON output from ffprobe
type FFprobeOutput struct {
	Streams []FFprobeStream `json:"streams"`
	Format  FFprobeFormat   `json:"format"`
}

// FFprobeStream represents a video/audio stream from ffprobe
type FFprobeStream struct {
	Index              int                    `json:"index"`
	CodecName          string                 `json:"codec_name"`
	CodecLongName      string                 `json:"codec_long_name"`
	CodecType          string                 `json:"codec_type"` // "video" or "audio"
	CodecTimeBase      string                 `json:"codec_time_base"`
	CodecTagString     string                 `json:"codec_tag_string"`
	Width              int                    `json:"width,omitempty"`
	Height             int                    `json:"height,omitempty"`
	CodedWidth         int                    `json:"coded_width,omitempty"`
	CodedHeight        int                    `json:"coded_height,omitempty"`
	HasBFrames         int                    `json:"has_b_frames,omitempty"`
	SampleAspectRatio  string                 `json:"sample_aspect_ratio,omitempty"`
	DisplayAspectRatio string                 `json:"display_aspect_ratio,omitempty"`
	PixFmt             string                 `json:"pix_fmt,omitempty"`
	Level              int                    `json:"level,omitempty"`
	RFrameRate         string                 `json:"r_frame_rate,omitempty"`
	AvgFrameRate       string                 `json:"avg_frame_rate,omitempty"`
	TimeBase           string                 `json:"time_base,omitempty"`
	DurationTS         int64                  `json:"duration_ts,omitempty"`
	Duration           string                 `json:"duration,omitempty"`
	BitRate            string                 `json:"bit_rate,omitempty"`
	BitsPerRawSample   string                 `json:"bits_per_raw_sample,omitempty"`
	NbFrames           string                 `json:"nb_frames,omitempty"`
	SampleFmt          string                 `json:"sample_fmt,omitempty"`
	SampleRate         string                 `json:"sample_rate,omitempty"`
	Channels           int                    `json:"channels,omitempty"`
	ChannelLayout      string                 `json:"channel_layout,omitempty"`
	Tags               map[string]interface{} `json:"tags,omitempty"`
}

// FFprobeFormat represents the container format from ffprobe
type FFprobeFormat struct {
	Filename       string                 `json:"filename"`
	NbStreams      int                    `json:"nb_streams"`
	NbPrograms     int                    `json:"nb_programs"`
	FormatName     string                 `json:"format_name"`
	FormatLongName string                 `json:"format_long_name"`
	Duration       string                 `json:"duration"`
	Size           string                 `json:"size"`
	BitRate        string                 `json:"bit_rate"`
	ProbeScore     int                    `json:"probe_score"`
	Tags           map[string]interface{} `json:"tags,omitempty"`
}

// MetadataExtractor handles video metadata extraction
type MetadataExtractor struct {
	ffmpeg *utils.FFmpegHelper
}

// NewMetadataExtractor creates a new metadata extractor
func NewMetadataExtractor(ffmpeg *utils.FFmpegHelper) *MetadataExtractor {
	return &MetadataExtractor{
		ffmpeg: ffmpeg,
	}
}

// Extract extracts comprehensive video metadata
func (me *MetadataExtractor) Extract(videoPath string) (*models.VideoMetadata, error) {
	// Get file size
	fileInfo, err := os.Stat(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	size := fileInfo.Size()

	// Get duration
	duration, err := me.ffmpeg.GetVideoDuration(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get duration: %w", err)
	}

	// Get resolution
	width, height, err := me.ffmpeg.GetResolution(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get resolution: %w", err)
	}

	// Get frame rate
	frameRate, err := me.ffmpeg.GetFrameRate(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get frame rate: %w", err)
	}

	// Get codec information
	codec, audioCodec, err := me.getCodecInfo(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get codec info: %w", err)
	}

	// Get bitrate
	bitrate, err := me.getBitrate(videoPath)
	if err != nil {
		// Non-fatal
		bitrate = 0
	}

	// Get format
	format := me.getFormat(videoPath)

	// Get audio tracks count
	audioTracks, err := me.getAudioTrackCount(videoPath)
	if err != nil {
		audioTracks = 0
	}

	// Check for subtitles
	hasSubtitles, err := me.hasSubtitles(videoPath)
	if err != nil {
		hasSubtitles = false
	}

	// Determine quality
	quality := me.determineQuality(width, height)

	metadata := &models.VideoMetadata{
		Duration:     duration,
		Width:        width,
		Height:       height,
		FrameRate:    frameRate,
		Codec:        codec,
		Bitrate:      bitrate,
		Size:         size,
		Format:       format,
		AudioCodec:   audioCodec,
		AudioTracks:  audioTracks,
		HasSubtitles: hasSubtitles,
		Quality:      quality,
	}

	return metadata, nil
}

// getCodecInfo extracts video and audio codec information
func (me *MetadataExtractor) getCodecInfo(videoPath string) (string, string, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to get video codec: %w", err)
	}

	videoCodec := strings.TrimSpace(string(output))

	// Get audio codec
	cmd = exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)

	output, err = cmd.Output()
	if err != nil {
		// Video might not have audio
		return videoCodec, "none", nil
	}

	audioCodec := strings.TrimSpace(string(output))

	return videoCodec, audioCodec, nil
}

// getBitrate extracts video bitrate
func (me *MetadataExtractor) getBitrate(videoPath string) (int64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=bit_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get bitrate: %w", err)
	}

	bitrateStr := strings.TrimSpace(string(output))
	bitrate, err := strconv.ParseInt(bitrateStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse bitrate: %w", err)
	}

	return bitrate, nil
}

// getFormat extracts video format
func (me *MetadataExtractor) getFormat(videoPath string) string {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=format_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		// Fallback to file extension
		parts := strings.Split(videoPath, ".")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
		return "unknown"
	}

	format := strings.TrimSpace(string(output))
	return format
}

// getAudioTrackCount counts audio tracks in video
func (me *MetadataExtractor) getAudioTrackCount(videoPath string) (int, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "a",
		"-show_entries", "stream=index",
		"-of", "csv=p=0",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0, nil
	}

	return len(lines), nil
}

// hasSubtitles checks if video has subtitle tracks
func (me *MetadataExtractor) hasSubtitles(videoPath string) (bool, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "s",
		"-show_entries", "stream=index",
		"-of", "csv=p=0",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return false, nil
	}

	outputStr := strings.TrimSpace(string(output))
	return len(outputStr) > 0, nil
}

// determineQuality determines video quality based on resolution
func (me *MetadataExtractor) determineQuality(width, height int) string {
	pixels := width * height

	if pixels >= 3840*2160 { // 4K
		return "4k"
	} else if pixels >= 1920*1080 { // Full HD
		return "high"
	} else if pixels >= 1280*720 { // HD
		return "medium"
	} else { // SD or lower
		return "low"
	}
}

// ValidateVideo validates video file integrity
func (me *MetadataExtractor) ValidateVideo(videoPath string) error {
	return me.ffmpeg.ValidateVideo(videoPath)
}

// GetDetailedMetadata extracts detailed metadata including streams info with proper JSON parsing
func (me *MetadataExtractor) GetDetailedMetadata(videoPath string) (map[string]interface{}, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe detailed analysis failed: %w", err)
	}

	// Parse JSON output into typed structs
	var ffprobeOutput FFprobeOutput
	if err := json.Unmarshal(output, &ffprobeOutput); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe JSON: %w", err)
	}

	// Convert to map[string]interface{} for flexible access
	metadata := make(map[string]interface{})

	// Add format information
	metadata["format_name"] = ffprobeOutput.Format.FormatName
	metadata["format_long_name"] = ffprobeOutput.Format.FormatLongName
	metadata["duration"] = ffprobeOutput.Format.Duration
	metadata["size"] = ffprobeOutput.Format.Size
	metadata["bit_rate"] = ffprobeOutput.Format.BitRate
	metadata["nb_streams"] = ffprobeOutput.Format.NbStreams
	metadata["format_tags"] = ffprobeOutput.Format.Tags

	// Separate video and audio streams for easy access
	var videoStreams []FFprobeStream
	var audioStreams []FFprobeStream
	var subtitleStreams []FFprobeStream

	for _, stream := range ffprobeOutput.Streams {
		switch stream.CodecType {
		case "video":
			videoStreams = append(videoStreams, stream)
		case "audio":
			audioStreams = append(audioStreams, stream)
		case "subtitle":
			subtitleStreams = append(subtitleStreams, stream)
		}
	}

	metadata["video_streams"] = videoStreams
	metadata["audio_streams"] = audioStreams
	metadata["subtitle_streams"] = subtitleStreams
	metadata["video_stream_count"] = len(videoStreams)
	metadata["audio_stream_count"] = len(audioStreams)
	metadata["subtitle_stream_count"] = len(subtitleStreams)

	// Add primary video stream info (first video stream)
	if len(videoStreams) > 0 {
		primaryVideo := videoStreams[0]
		metadata["primary_video_codec"] = primaryVideo.CodecName
		metadata["primary_video_codec_long"] = primaryVideo.CodecLongName
		metadata["primary_video_width"] = primaryVideo.Width
		metadata["primary_video_height"] = primaryVideo.Height
		metadata["primary_video_frame_rate"] = primaryVideo.RFrameRate
		metadata["primary_video_bit_rate"] = primaryVideo.BitRate
		metadata["primary_video_pixel_format"] = primaryVideo.PixFmt
	}

	// Add primary audio stream info (first audio stream)
	if len(audioStreams) > 0 {
		primaryAudio := audioStreams[0]
		metadata["primary_audio_codec"] = primaryAudio.CodecName
		metadata["primary_audio_codec_long"] = primaryAudio.CodecLongName
		metadata["primary_audio_sample_rate"] = primaryAudio.SampleRate
		metadata["primary_audio_channels"] = primaryAudio.Channels
		metadata["primary_audio_channel_layout"] = primaryAudio.ChannelLayout
		metadata["primary_audio_bit_rate"] = primaryAudio.BitRate
	}

	// Add complete parsed structure for advanced use cases
	metadata["ffprobe_output"] = ffprobeOutput

	return metadata, nil
}
