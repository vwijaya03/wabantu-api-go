// Package simulator holds pure validation/derivation for the judge chat simulator
// (webhook/simulator.go). Kept dependency-free so it is unit-testable without Encore.
package simulator

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxTextRunes  = 1000
	MaxNameRunes  = 60
	MaxImageBytes = 2 << 20
	DefaultBuyer  = "Pembeli Demo"
	RatePerMinute = 20
	ChannelName   = "Simulator Juri"
	ChannelPhone  = "SIMULATOR"
)

var reSession = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

var allowedMime = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// Image is the optional uploaded image (payment proof / product photo).
type Image struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mimeType"`
}

// Input is the buyer message. Deliberately no price/status/order fields.
type Input struct {
	SessionID string `json:"sessionId"`
	BuyerName string `json:"buyerName"`
	Text      string `json:"text"`
	Image     *Image `json:"image"`
}

// Normalized is the validated message.
type Normalized struct {
	SessionID string
	BuyerName string
	Text      string
	ImageData []byte
	ImageMime string
}

func (n Normalized) Type() string {
	if len(n.ImageData) > 0 {
		return "image"
	}
	return "text"
}

// ValidateSession checks the client-generated buyer id.
func ValidateSession(id string) error {
	if !reSession.MatchString(id) {
		return errors.New("sessionId harus 6-64 karakter [A-Za-z0-9_-]")
	}
	return nil
}

// Validate normalizes and validates an Input.
func Validate(in Input) (Normalized, error) {
	var out Normalized
	sid := strings.TrimSpace(in.SessionID)
	if err := ValidateSession(sid); err != nil {
		return out, err
	}
	out.SessionID = sid
	name := strings.TrimSpace(in.BuyerName)
	if name == "" {
		name = DefaultBuyer
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return out, fmt.Errorf("buyerName maksimal %d karakter", MaxNameRunes)
	}
	out.BuyerName = stripControl(name)
	text := strings.TrimSpace(in.Text)
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return out, fmt.Errorf("text maksimal %d karakter", MaxTextRunes)
	}
	if !utf8.ValidString(text) {
		return out, errors.New("text bukan UTF-8 valid")
	}
	out.Text = strings.TrimSpace(stripControl(text))
	if in.Image != nil && strings.TrimSpace(in.Image.Base64) != "" {
		mime := strings.ToLower(strings.TrimSpace(in.Image.MimeType))
		if !allowedMime[mime] {
			return out, errors.New("mimeType harus image/jpeg, image/png, atau image/webp")
		}
		raw := in.Image.Base64
		if i := strings.Index(raw, ","); strings.HasPrefix(raw, "data:") && i > 0 {
			raw = raw[i+1:]
		}
		if base64.StdEncoding.DecodedLen(len(raw)) > MaxImageBytes+4 {
			return out, fmt.Errorf("gambar maksimal %d MB", MaxImageBytes>>20)
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return out, errors.New("base64 gambar tidak valid")
		}
		if len(data) == 0 || len(data) > MaxImageBytes {
			return out, fmt.Errorf("gambar maksimal %d MB", MaxImageBytes>>20)
		}
		sniff := http.DetectContentType(data)
		if !allowedMime[sniff] && !(mime == "image/webp" && sniff == "application/octet-stream") {
			return out, errors.New("isi file bukan gambar yang didukung")
		}
		out.ImageData, out.ImageMime = data, mime
	}
	if out.Text == "" && len(out.ImageData) == 0 {
		return out, errors.New("pesan kosong: isi text atau image")
	}
	return out, nil
}

// BuyerPhone derives a stable fake MSISDN per (tenant, session). The "62000" prefix is not a
// valid Indonesian mobile range, so it can never collide with a real buyer.
func BuyerPhone(tenantID, sessionID string) string {
	h := sha256.Sum256([]byte(tenantID + "|" + sessionID))
	n := binary.BigEndian.Uint64(h[:8]) % 10_000_000_000
	return fmt.Sprintf("62000%010d", n)
}

func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
