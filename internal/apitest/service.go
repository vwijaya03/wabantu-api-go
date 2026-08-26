package apitest

import "context"

// Ping is a private marker endpoint so this package is an Encore service and may
// call other APIs from integration tests in this package.
//
//encore:api private method=GET path=/internal/apitest/ping
func Ping(ctx context.Context) (*PingResponse, error) {
	return &PingResponse{OK: true}, nil
}

type PingResponse struct {
	OK bool `json:"ok"`
}
