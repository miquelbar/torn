package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"torn/internal/strutil"
)

// ExtractedContent represents the parsed text and tool interactions from a turn.
type ExtractedContent struct {
	Text         string
	Tools        []string
	FilesTouched []string
	Error        string
}

// ParseContentBlock flexibly parses polymorphic content from LLM messages.
// Handles string, []any of blocks, map[string]any, or json.RawMessage.
func ParseContentBlock(raw any) ExtractedContent {
	var res ExtractedContent
	if raw == nil {
		return res
	}

	switch v := raw.(type) {
	case string:
		res.Text = strings.TrimSpace(v)
		return res

	case []byte:
		var parsed any
		if err := json.Unmarshal(v, &parsed); err == nil {
			return ParseContentBlock(parsed)
		}
		res.Text = string(v)
		return res

	case []any:
		var textParts []string
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok {
				if str, ok := item.(string); ok && str != "" {
					textParts = append(textParts, str)
				}
				continue
			}

			blockType, _ := block["type"].(string)
			switch blockType {
			case "text", "input_text", "output_text", "Text":
				if txt, ok := block["text"].(string); ok && strings.TrimSpace(txt) != "" {
					textParts = append(textParts, strings.TrimSpace(txt))
				}

			case "tool_use", "tool_call", "custom_tool_call":
				toolName, _ := block["name"].(string)
				if toolName == "" {
					toolName, _ = block["function_name"].(string)
				}
				if toolName != "" {
					summary := toolName
					// Extract touched file or command if present in input/arguments
					argSummary := extractArgSummary(block["input"])
					if argSummary == "" {
						argSummary = extractArgSummary(block["arguments"])
					}
					if argSummary != "" {
						summary = fmt.Sprintf("%s(%s)", toolName, argSummary)
					}
					res.Tools = append(res.Tools, summary)
				}

			case "tool_result":
				if isErr, ok := block["is_error"].(bool); ok && isErr {
					if c, ok := block["content"].(string); ok {
						res.Error = strings.TrimSpace(c)
					}
				}

			case "thinking":
				// If no other text is present, thinking can be used as fallback
				if th, ok := block["thinking"].(string); ok && strings.TrimSpace(th) != "" {
					if len(textParts) == 0 {
						textParts = append(textParts, strings.TrimSpace(th))
					}
				}
			}
		}
		res.Text = strings.Join(textParts, "\n\n")
		return res

	case map[string]any:
		if txt, ok := v["text"].(string); ok {
			res.Text = strings.TrimSpace(txt)
		} else if cnt, ok := v["content"]; ok {
			return ParseContentBlock(cnt)
		}
		return res
	}

	return res
}

func extractArgSummary(rawArgs any) string {
	if rawArgs == nil {
		return ""
	}

	var m map[string]any
	switch a := rawArgs.(type) {
	case string:
		if err := json.Unmarshal([]byte(a), &m); err != nil {
			return strutil.Truncate(strings.TrimSpace(a), 50, "...")
		}
	case map[string]any:
		m = a
	}

	if m == nil {
		return ""
	}

	// Prioritize common paths, targets, commands, and summaries
	keys := []string{
		"TargetFile", "target_file", "AbsolutePath", "absolute_path",
		"DirectoryPath", "directory_path", "file_path", "filePath",
		"path", "Path", "file", "File", "CommandLine", "command",
		"cmd", "query", "Query", "pattern", "Pattern", "Url", "url",
		"toolSummary",
	}

	for _, key := range keys {
		if val, ok := m[key].(string); ok && strings.TrimSpace(val) != "" {
			return strutil.Truncate(strings.TrimSpace(val), 60, "...")
		}
	}

	return ""
}
