package ai

import (
	"strings"
	"time"
)

// GreetingPeriod is the time-of-day bucket for Indonesian salutations.
type GreetingPeriod string

const (
	GreetMorning   GreetingPeriod = "morning"   // selamat pagi
	GreetAfternoon GreetingPeriod = "afternoon" // selamat siang
	GreetEvening   GreetingPeriod = "evening"   // selamat sore
	GreetNight     GreetingPeriod = "night"     // selamat malam
)

var jakartaTZ = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return loc
}()

// periodPatterns — longest match first; customer intent ("bilang malam") before incidental words ("balasannya siang").
var periodPatterns = []struct {
	substr string
	period GreetingPeriod
}{
	{"selamat malam", GreetNight},
	{"bilang malam", GreetNight},
	{"kata malam", GreetNight},
	{"selamat sore", GreetEvening},
	{"bilang sore", GreetEvening},
	{"selamat siang", GreetAfternoon},
	{"bilang siang", GreetAfternoon},
	{"selamat pagi", GreetMorning},
	{"bilang pagi", GreetMorning},
}

var periodPhrase = map[GreetingPeriod]string{
	GreetMorning:   "Selamat pagi",
	GreetAfternoon: "Selamat siang",
	GreetEvening:   "Selamat sore",
	GreetNight:     "Selamat malam",
}

// DetectGreetingPeriodFromText reads the salutation the customer used (not server clock).
func DetectGreetingPeriodFromText(userText string) (GreetingPeriod, bool) {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return "", false
	}
	for _, p := range periodPatterns {
		if strings.Contains(text, p.substr) {
			return p.period, true
		}
	}
	standalone := map[string]GreetingPeriod{
		"malam": GreetNight, "pagi": GreetMorning, "siang": GreetAfternoon, "sore": GreetEvening,
	}
	for word, period := range standalone {
		if text == word || strings.HasPrefix(text, word+" ") {
			return period, true
		}
	}
	return "", false
}

// CurrentGreetingPeriodWIB picks a default salutation from Indonesia local time when the customer did not specify one.
func CurrentGreetingPeriodWIB(now time.Time) GreetingPeriod {
	h := now.In(jakartaTZ).Hour()
	switch {
	case h >= 5 && h < 11:
		return GreetMorning
	case h >= 11 && h < 15:
		return GreetAfternoon
	case h >= 15 && h < 19:
		return GreetEvening
	default:
		return GreetNight
	}
}

func greetingPhrase(period GreetingPeriod) string {
	if p, ok := periodPhrase[period]; ok {
		return p
	}
	return periodPhrase[GreetAfternoon]
}

// GreetingReply mirrors the customer's time-of-day or uses WIB; ignores static "Selamat siang" defaults when they said malam/pagi.
func GreetingReply(userText, tone, customTemplate string) string {
	period, explicit := DetectGreetingPeriodFromText(userText)
	if !explicit {
		if t := strings.TrimSpace(customTemplate); t != "" {
			return t
		}
		period = CurrentGreetingPeriodWIB(time.Now())
	}
	return formatGreetingReply(period, tone == "formal")
}

func formatGreetingReply(period GreetingPeriod, formal bool) string {
	phrase := greetingPhrase(period)
	if formal {
		return phrase + ", kak. Ada yang bisa kami bantu?"
	}
	return phrase + " kak! Ada yang bisa aku bantu?"
}

// IsCasualChatOpener — short WA openers ("malam gan", "malam min") that are not order-form answers.
func IsCasualChatOpener(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" || len(strings.Fields(text)) > 5 {
		return false
	}
	for _, p := range []string{"malam ", "pagi ", "siang ", "sore ", "halo ", "hai ", "hei "} {
		if text == strings.TrimSuffix(p, " ") || strings.HasPrefix(text, p) {
			return true
		}
	}
	if len(strings.Fields(text)) <= 3 {
		for _, w := range strings.Fields(text) {
			switch w {
			case "malam", "pagi", "siang", "sore", "halo", "hai", "min", "gan", "kak", "ka", "bro":
				return true
			}
		}
	}
	return false
}

// IsGreetingFeedback detects complaints about a wrong salutation (e.g. "bilang malam kok balasannya siang").
func IsGreetingFeedback(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	hasTime := strings.Contains(text, "malam") || strings.Contains(text, "siang") ||
		strings.Contains(text, "pagi") || strings.Contains(text, "sore")
	if !hasTime {
		return false
	}
	for _, kw := range []string{"balas", "balasan", "kok", "salah", "keliru", "kekeliruan", "bilang", "kata"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// GreetingFeedbackReply apologizes and uses the salutation the customer intended.
func GreetingFeedbackReply(userText, tone string) string {
	period, ok := DetectGreetingPeriodFromText(userText)
	if !ok {
		period = GreetNight
		if strings.Contains(strings.ToLower(userText), "siang") {
			period = GreetAfternoon
		}
	}
	phrase := greetingPhrase(period)
	formal := tone == "formal"
	if formal {
		return "Maaf kak, " + phrase + ", kak. Tadi ada kekeliruan salam kami. Ada yang bisa kami bantu?"
	}
	return "Maaf kak, " + phrase + " kak! Maaf tadi salamnya keliru — ada yang bisa aku bantu? 🙏"
}
