package events

import (
	"bytes"
	"encoding/json"
	"strings"
)

// flexStringList accepts JSON string ("A, B") or array ["A","B"] — common in vision output.
type flexStringList []string

func (f *flexStringList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = nil
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = splitCommaList(s)
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*f = arr
	return nil
}

type staffVisionRawItem struct {
	FullName          string         `json:"fullName"`
	Nama              string         `json:"nama"`
	NamaTerapis       string         `json:"namaTerapis"`
	Name              string         `json:"name"`
	Role              string         `json:"role"`
	Peran             string         `json:"peran"`
	TherapyNames      flexStringList `json:"therapyNames"`
	Terapi            flexStringList `json:"terapi"`
	TerapiYangDipilih flexStringList `json:"terapiYangDipilih"`
	TerapiYangAndaPilih flexStringList `json:"terapiYangAndaPilih"`
	VolunteerRoleName string         `json:"volunteerRoleName"`
	PeranRelawan      string         `json:"peranRelawan"`
	IsPencatat        bool           `json:"isPencatat"`
	Attendance        string         `json:"attendance"`
	AttendanceLabel   string         `json:"attendanceLabel"`
	ApakahBisaDatang  string         `json:"apakahBisaDatang"`
	Kehadiran         string         `json:"kehadiran"`
	Include           bool           `json:"include"`
}

func parseStaffVisionResponse(raw string) ([]StaffImageDraftItem, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, err
	}

	for _, key := range []string{"items", "staff", "rows", "terapis", "data"} {
		if chunk, ok := root[key]; ok {
			items, err := decodeStaffVisionItems(chunk)
			if err != nil {
				return nil, err
			}
			if len(items) > 0 {
				return items, nil
			}
		}
	}

	// Top-level array
	if raw[0] == '[' {
		return decodeStaffVisionItems([]byte(raw))
	}

	return nil, nil
}

func decodeStaffVisionItems(data []byte) ([]StaffImageDraftItem, error) {
	var rawItems []staffVisionRawItem
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return nil, err
	}
	var out []StaffImageDraftItem
	for _, r := range rawItems {
		it := normalizeStaffVisionItem(r)
		if it.FullName == "" {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func normalizeStaffVisionItem(r staffVisionRawItem) StaffImageDraftItem {
	name := firstNonEmpty(
		r.FullName, r.Nama, r.NamaTerapis, r.Name,
	)
	therapies := mergeStringLists(
		[]string(r.TherapyNames),
		[]string(r.Terapi),
		[]string(r.TerapiYangDipilih),
		[]string(r.TerapiYangAndaPilih),
	)
	attendance := firstNonEmpty(r.AttendanceLabel, r.ApakahBisaDatang, r.Kehadiran, r.Attendance)

	role := strings.TrimSpace(r.Role)
	if role == "" {
		role = strings.TrimSpace(r.Peran)
	}
	role = inferStaffRoleFromVision(name, role, therapies)

	return StaffImageDraftItem{
		FullName:          strings.TrimSpace(name),
		Role:              role,
		TherapyNames:      therapies,
		VolunteerRoleName: firstNonEmpty(r.VolunteerRoleName, r.PeranRelawan),
		IsPencatat:        r.IsPencatat,
		AttendanceLabel:   strings.TrimSpace(attendance),
		Include:           r.Include,
	}
}

func inferStaffRoleFromVision(name, role string, therapies []string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "terapis", "relawan", "volunteer", "shijie", "daoshi", "fashi":
		if role == "volunteer" {
			return "relawan"
		}
		return role
	case "therapist", "terapist":
		return "terapis"
	}
	lowerName := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(lowerName, "daoshi"):
		return "daoshi"
	case strings.HasPrefix(lowerName, "fashi"):
		return "fashi"
	case strings.HasPrefix(lowerName, "shijie"):
		return "shijie"
	}
	if len(therapies) > 0 {
		return "terapis"
	}
	if role != "" {
		return role
	}
	return "relawan"
}

func mapStaffAttendanceLabel(label string) (status, notes string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "PRESENT", ""
	}
	lower := strings.ToLower(label)
	switch {
	case lower == "bisa", lower == "ya", lower == "yes", lower == "hadir":
		return "PRESENT", ""
	case strings.Contains(lower, "tidak"), lower == "no", lower == "absent":
		return "NOT_PRESENT", label
	default:
		return "PARTIAL", label
	}
}

func splitCommaList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func mergeStringLists(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, s := range list {
			s = strings.TrimSpace(s)
			if s == "" || seen[strings.ToLower(s)] {
				continue
			}
			seen[strings.ToLower(s)] = true
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func staffVisionEmptyMessage(warnings []string) string {
	msg := "tidak ada staf terdeteksi"
	if len(warnings) > 0 {
		return msg + ": " + strings.Join(warnings, "; ")
	}
	return msg + " — pastikan foto menampilkan tabel dengan kolom Nama Terapis dan Terapi Yang Anda Pilih"
}
