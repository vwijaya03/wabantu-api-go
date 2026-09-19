package buyerflow

import "strings"

// PathChannelMeta — balloon perbaikan saluran (chat putus), bukan niat baru.
const PathChannelMeta = "channel_meta"

var channelRepairPhrases = []string{
	"putus putus", "putus-putus", "chatnya putus", "chat putus",
	"pesan terputus", "pesan putus", "ketik ulang",
	"messages sent separately", "connection interrupted",
}

var channelRepairFillers = map[string]struct{}{
	"maaf": {}, "sorry": {}, "ya": {}, "yah": {}, "kak": {}, "min": {},
	"bang": {}, "gan": {}, "chatnya": {}, "chat": {}, "nya": {},
	"dong": {}, "sih": {}, "nih": {}, "ini": {}, "lagi": {},
}

// IsChannelRepairMeta — sisa kata setelah strip frasa saluran hanya filler (maaf/kak).
func IsChannelRepairMeta(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	hit := false
	stripped := text
	for _, p := range channelRepairPhrases {
		if strings.Contains(text, p) {
			hit = true
			stripped = strings.ReplaceAll(stripped, p, " ")
		}
	}
	if !hit {
		return false
	}
	stripped = strings.TrimSpace(nonAlphaNum.ReplaceAllString(stripped, " "))
	for _, w := range strings.Fields(stripped) {
		if _, ok := channelRepairFillers[w]; !ok {
			return false
		}
	}
	return true
}
