package admin

import (
	"context"

	"encore.app/wabantu/ai"
)

type RetrievalIncidentsResponse struct {
	Incidents []ai.RetrievalIncident `json:"incidents"`
}

//encore:api auth method=GET path=/api/v1/admin/ai-retrieval/incidents tag:super_admin
func GetRetrievalIncidents(ctx context.Context) (*RetrievalIncidentsResponse, error) {
	if _, err := requireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	incidents, err := ai.RecentRetrievalIncidents(ctx, 50)
	if err != nil {
		return &RetrievalIncidentsResponse{Incidents: []ai.RetrievalIncident{}}, nil
	}
	return &RetrievalIncidentsResponse{Incidents: incidents}, nil
}
