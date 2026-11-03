package provider

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var userRequestRegex = regexp.MustCompile(`(?s)<\/?USER_REQUEST>`)
var truncatedMarkerRegex = regexp.MustCompile(`(?i)<truncated\s+[^>]+>`)
var additionalMetaRegex = regexp.MustCompile(`(?s)<ADDITIONAL_METADATA>.*?(?:</ADDITIONAL_METADATA>|$)`)
var userSettingsRegex = regexp.MustCompile(`(?s)<USER_SETTINGS_CHANGE>.*?(?:</USER_SETTINGS_CHANGE>|$)`)
var agyModelChangeRegex = regexp.MustCompile(`(?i)` + "`" + `Model Selection` + "`" + `\s+from\s+.*?\s+to\s+(.*?)\.\s*(?:[A-Z\n<]|$)`)

func (a *AgyProvider) lookupConversationWorkspace(convID string) string {
	f, err := os.Open(filepath.Join(a.baseDir, "history.jsonl"))
	if err != nil {
		return ""
	}
	defer f.Close()

	cleanID := strings.Trim(strings.TrimSpace(convID), `"'`)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var item struct {
			ConversationID string `json:"conversationId"`
			SessionID      string `json:"session_id"`
			Workspace      string `json:"workspace"`
			Cwd            string `json:"cwd"`
			Project        string `json:"project"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil {
			id := item.ConversationID
			if id == "" {
				id = item.SessionID
			}
			if strings.Trim(strings.TrimSpace(id), `"'`) == cleanID {
				ws := item.Workspace
				if ws == "" {
					ws = item.Cwd
				}
				if ws == "" {
					ws = item.Project
				}
				return strings.Trim(strings.TrimSpace(ws), `"'`)
			}
		}
	}
	return ""
}

func cleanUserPrompt(s string) string {
	res := userRequestRegex.ReplaceAllString(s, "")
	res = additionalMetaRegex.ReplaceAllString(res, "")
	res = userSettingsRegex.ReplaceAllString(res, "")
	res = truncatedMarkerRegex.ReplaceAllString(res, "")
	return strings.TrimSpace(res)
}

func cleanThinking(s string) string {
	res := truncatedMarkerRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(res)
}
