package response

// Envelope matches NestJS TransformInterceptor success shape.
type Envelope[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

// Wrap returns { success: true, data } for JSON handlers (auth raw, webhooks).
func Wrap[T any](data T) Envelope[T] {
	return Envelope[T]{Success: true, Data: data}
}
