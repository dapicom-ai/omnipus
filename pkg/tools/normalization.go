package tools

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/dapicom-ai/omnipus/pkg/media"
)

const (
	largeBase64OmittedMessage = `{"status":"success","message":"The requested output has already been delivered to the user in the current chat. Do not repeat or describe it."}`
	inlineMediaOmittedMessage = `{"status":"success","message":"Media content delivered to user. Do not repeat or describe it."}`
	inlineMediaStoredMessage  = `{"status":"success","type":"%s","message":"Media content delivered to user. Do not repeat or describe it."}`
)

var (
	inlineMarkdownDataURLRe = regexp.MustCompile(`!\[[^\]]*\]\((data:[^)]+)\)`)
	inlineRawDataURLRe      = regexp.MustCompile(`data:[^;\s]+;base64,[A-Za-z0-9+/=\r\n]+`)
)

func normalizeToolResult(
	result *ToolResult,
	toolName string,
	store media.MediaStore,
	channel string,
	chatID string,
) *ToolResult {
	if result == nil {
		return nil
	}

	notes := make([]string, 0, 2)
	seen := make(map[string]struct{})

	if store != nil && channel != "" && chatID != "" {
		var refs []string
		var extractedNotes []string

		result.ForLLM, refs, extractedNotes = extractInlineMediaRefs(
			result.ForLLM,
			toolName,
			store,
			channel,
			chatID,
			seen,
		)
		result.Media = append(result.Media, refs...)
		notes = append(notes, extractedNotes...)

		result.ForUser, refs, extractedNotes = extractInlineMediaRefs(
			result.ForUser,
			toolName,
			store,
			channel,
			chatID,
			seen,
		)
		result.Media = append(result.Media, refs...)
		notes = append(notes, extractedNotes...)
	}

	result.ForLLM = sanitizeToolLLMContent(result.ForLLM)

	// Append notes to ForLLM, but only if ForLLM is plain text.
	// If ForLLM is already JSON (starts with '{'), don't append notes as raw text
	// because that would produce invalid JSON and break providers like Anthropic/Azure.
	if len(result.Media) > 0 && len(notes) > 0 {
		trimmed := strings.TrimSpace(result.ForLLM)
		if trimmed == "" {
			result.ForLLM = strings.Join(notes, "\n")
		} else if !strings.HasPrefix(trimmed, "{") {
			result.ForLLM = trimmed + "\n" + strings.Join(notes, "\n")
		}
		// If ForLLM is JSON, notes are skipped — the JSON placeholder already
		// tells the LLM that media was delivered.
	}
	if len(result.Media) > 0 && strings.TrimSpace(result.ForLLM) == "" {
		result.ForLLM = `{"status":"success","message":"Media content delivered to user. Do not repeat or describe it."}`
	}

	// When normalization registered media AND fully consumed the ForLLM content
	// (replaced with a placeholder), mark the result as handled so the agent loop
	// delivers the media to the chat channel. Don't override if the tool already
	// set ResponseHandled, or if there's meaningful ForLLM content remaining.
	if len(result.Media) > 0 && !result.ResponseHandled {
		llmIsPlaceholder := strings.Contains(result.ForLLM, "delivered to user") ||
			strings.HasPrefix(result.ForLLM, "[Tool returned") ||
			result.ForLLM == inlineMediaOmittedMessage ||
			result.ForLLM == largeBase64OmittedMessage
		if llmIsPlaceholder {
			result.ResponseHandled = true
		}
	}

	return result
}

func sanitizeToolLLMContent(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	if inlineMarkdownDataURLRe.MatchString(trimmed) || inlineRawDataURLRe.MatchString(trimmed) {
		cleaned := inlineMarkdownDataURLRe.ReplaceAllString(trimmed, "")
		cleaned = inlineRawDataURLRe.ReplaceAllString(cleaned, "")
		cleaned = strings.TrimSpace(cleaned)
		if cleaned == "" {
			return inlineMediaOmittedMessage
		}
		return cleaned + "\n" + inlineMediaOmittedMessage
	}
	if looksLikeLargeBase64Payload(trimmed) {
		return largeBase64OmittedMessage
	}
	return text
}

func looksLikeLargeBase64Payload(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 1024 {
		return false
	}

	nonSpace := 0
	base64Like := 0
	spaceCount := 0

	for _, r := range trimmed {
		if unicode.IsSpace(r) {
			spaceCount++
			continue
		}
		nonSpace++
		if (r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '+' || r == '/' || r == '=' {
			base64Like++
		}
	}

	if nonSpace == 0 {
		return false
	}

	ratio := float64(base64Like) / float64(nonSpace)
	return ratio >= 0.97 && spaceCount <= len(trimmed)/128
}

func extractInlineMediaRefs(
	text string,
	toolName string,
	store media.MediaStore,
	channel string,
	chatID string,
	seen map[string]struct{},
) (cleaned string, refs []string, notes []string) {
	cleaned = text

	matches := inlineMarkdownDataURLRe.FindAllStringSubmatch(cleaned, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		dataURL := match[1]
		ref, note := storeInlineDataURL(toolName, store, channel, chatID, dataURL, seen)
		if ref != "" {
			refs = append(refs, ref)
		}
		if note != "" {
			notes = append(notes, note)
		}
		cleaned = strings.ReplaceAll(cleaned, match[0], "")
	}

	rawMatches := inlineRawDataURLRe.FindAllString(cleaned, -1)
	for _, dataURL := range rawMatches {
		ref, note := storeInlineDataURL(toolName, store, channel, chatID, dataURL, seen)
		if ref != "" {
			refs = append(refs, ref)
		}
		if note != "" {
			notes = append(notes, note)
		}
		cleaned = strings.ReplaceAll(cleaned, dataURL, "")
	}

	return strings.TrimSpace(cleaned), refs, notes
}

func storeInlineDataURL(
	toolName string,
	store media.MediaStore,
	channel string,
	chatID string,
	dataURL string,
	seen map[string]struct{},
) (ref string, note string) {
	dataURL = strings.TrimSpace(dataURL)
	if _, ok := seen[dataURL]; ok {
		return "", ""
	}
	seen[dataURL] = struct{}{}

	if !strings.HasPrefix(strings.ToLower(dataURL), "data:") {
		return "", ""
	}

	comma := strings.IndexByte(dataURL, ',')
	if comma <= 5 {
		return "", "[Tool returned inline media content that could not be parsed.]"
	}

	metaPart := dataURL[:comma]
	payload := dataURL[comma+1:]
	if !strings.Contains(strings.ToLower(metaPart), ";base64") {
		return "", "[Tool returned inline media content that was not base64-encoded.]"
	}

	mimeType := strings.TrimSpace(strings.TrimPrefix(metaPart, "data:"))
	if semi := strings.IndexByte(mimeType, ';'); semi >= 0 {
		mimeType = mimeType[:semi]
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	payload = strings.NewReplacer("\n", "", "\r", "", "\t", "", " ", "").Replace(payload)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Sprintf("[Tool returned inline media content (%s) that could not be decoded.]", mimeType)
	}

	dir := media.TempDir()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Sprintf("[Tool returned inline media content (%s) but it could not be stored.]", mimeType)
	}

	ext := extensionForMIMEType(mimeType)
	tmpFile, err := os.CreateTemp(dir, "tool-inline-*"+ext)
	if err != nil {
		return "", fmt.Sprintf("[Tool returned inline media content (%s) but it could not be stored.]", mimeType)
	}
	tmpPath := tmpFile.Name()
	if _, err = tmpFile.Write(decoded); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Sprintf("[Tool returned inline media content (%s) but it could not be stored.]", mimeType)
	}
	if err = tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Sprintf("[Tool returned inline media content (%s) but it could not be stored.]", mimeType)
	}

	filename := sanitizeIdentifierComponent(toolName) + ext
	scope := fmt.Sprintf(
		"tool:inline:%s:%s:%s:%d",
		sanitizeIdentifierComponent(toolName),
		channel,
		chatID,
		time.Now().UnixNano(),
	)

	ref, err = store.Store(tmpPath, media.MediaMeta{
		Filename:    filename,
		ContentType: mimeType,
		Source:      fmt.Sprintf("tool:inline:%s", sanitizeIdentifierComponent(toolName)),
	}, scope)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Sprintf("[Tool returned inline media content (%s) but it could not be registered.]", mimeType)
	}

	return ref, fmt.Sprintf(inlineMediaStoredMessage, mimeType)
}

func extensionForMIMEType(mimeType string) string {
	if mimeType == "" {
		return ".bin"
	}
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}

	switch strings.ToLower(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	default:
		return filepath.Ext(mimeType)
	}
}
