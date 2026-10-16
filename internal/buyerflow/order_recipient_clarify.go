package buyerflow

import (
	"strings"
	"unicode"
)

const maxClarifyNameLen = 60

// plausibleRecipientName — nama manusia singkat (huruf/spasi/tanda baca nama), bukan kalimat atau angka.
func plausibleRecipientName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxClarifyNameLen {
		return false
	}
	letters := 0
	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
			letters++
		case r == ' ' || r == '.' || r == '\'' || r == '-':
		default:
			return false
		}
	}
	return letters >= 2
}

// recipientClarifyPhoneReply — nama sudah ada tapi nomor belum valid: akui nama, minta nomor saja
// (format 08… = +62), bukan template AskRecipient penuh. Tidak menulis log / PII.
func recipientClarifyPhoneReply(st OrderState, userText string, formal bool) string {
	name := strings.TrimSpace(st.RecipientName)
	if ResolveBuyerLanguage(userText, CurrentSavedBuyerLang()) == LangEN {
		if formal {
			return "Thank you, we have the name " + name + ". For delivery we need an active mobile number in Indonesia — " +
				"usually starting with 08 (that's +62 without the country code).\n" +
				"Example: 081234567890 or Phone: 081234567890"
		}
		return "Thanks, I've got the name " + name + ". For delivery we need an active mobile number in Indonesia — " +
			"usually starting with 08 (that's +62 without the country code).\n" +
			"Example: 081234567890 or Phone: 081234567890"
	}
	if formal {
		return "Terima kasih kak, nama " + name + " sudah kami catat. Mohon kirim nomor HP/WA aktif ya — " +
			"format lokal 08… (angka 0 di depan = kode negara +62).\n" +
			"Contoh: 081234567890 atau HP: 081234567890"
	}
	return "Makasih kak, nama " + name + " sudah dicatat. Tinggal nomor HP/WA aktif ya — " +
		"format lokal 08… (0 di depan = kode negara +62).\n" +
		"Contoh: 081234567890 atau HP: 081234567890"
}
