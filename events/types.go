package events

// Named response types for Encore API schemas (anonymous structs are not allowed).

type EventScheduleResponse struct {
	Slots    []TimeSlot `json:"slots"`
	Patients []Patient  `json:"patients"`
}

type ListAssignmentsResponse struct {
	Items []Assignment `json:"items"`
	Total int          `json:"total"`
}

type ListEventTherapySettingsResponse struct {
	Items []EventTherapySetting `json:"items"`
}

type GenerateTimeSlotsResponse struct {
	Created  int      `json:"created"`
	Warnings []string `json:"warnings,omitempty"`
}

type ListTimeSlotsResponse struct {
	Items []TimeSlot `json:"items"`
}
