package salesbrain

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// The ship-today window is read from the tenant's FAQ ("pembayaran antara pukul 08:00 hingga
// 12:00 WIB dikirim di hari yang sama"). When it is not there, no STATE line is written and the
// model keeps answering from the FAQ text alone.
var (
	shipWindowRe  = regexp.MustCompile(`(?i)(\d{1,2})[.:](\d{2})\s*(?:wib)?\s*(?:hingga|sampai|s/d|sd|-|–|to|until)\s*(?:pukul\s*|jam\s*)?(\d{1,2})[.:](\d{2})`)
	shipCutoffRe  = regexp.MustCompile(`(?i)(?:sebelum|before|cut[- ]?off|batas)\D{0,20}(\d{1,2})[.:](\d{2})`)
	shipContextRe = regexp.MustCompile(`(?i)kirim|ship|dispatch`)
)

const defaultShipOpenMinutes = 8 * 60

// shipWindow returns the same-day window (minutes since midnight) found in the FAQ.
func shipWindow(kb []KBFact) (open, cutoff int, ok bool) {
	minutes := func(h, m string) (int, bool) {
		hh, err1 := strconv.Atoi(h)
		mm, err2 := strconv.Atoi(m)
		if err1 != nil || err2 != nil || hh > 23 || mm > 59 {
			return 0, false
		}
		return hh*60 + mm, true
	}
	for _, k := range kb {
		text := k.Question + " " + k.Answer
		if !shipContextRe.MatchString(text) {
			continue
		}
		if m := shipWindowRe.FindStringSubmatch(k.Answer); m != nil {
			o, ok1 := minutes(m[1], m[2])
			c, ok2 := minutes(m[3], m[4])
			if ok1 && ok2 && c > o {
				return o, c, true
			}
		}
		if m := shipCutoffRe.FindStringSubmatch(k.Answer); m != nil {
			if c, ok1 := minutes(m[1], m[2]); ok1 {
				return defaultShipOpenMinutes, c, true
			}
		}
	}
	return 0, 0, false
}

// ShipIfPaidNow is STATE.pengiriman_jika_bayar_sekarang, decided by code from the WIB clock so the
// model never reasons about the time itself: "hari ini" inside the window of a weekday, "hari ini
// bila dibayar antara 08:00–12:00" before it, "hari kerja berikutnya" after it or on a weekend.
// "" when the FAQ has no parseable window.
func ShipIfPaidNow(nowWIB time.Time, kb []KBFact) string {
	open, cutoff, ok := shipWindow(kb)
	if !ok {
		return ""
	}
	if wd := nowWIB.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return "hari kerja berikutnya"
	}
	now := nowWIB.Hour()*60 + nowWIB.Minute()
	switch {
	case now < open:
		return fmt.Sprintf("hari ini bila dibayar antara %s–%s", clock(open), clock(cutoff))
	case now < cutoff:
		return "hari ini"
	}
	return "hari kerja berikutnya"
}

func clock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
