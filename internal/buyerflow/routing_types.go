package buyerflow

// MessageComplexity drives Haiku vs Sonnet within hybrid plans.
type MessageComplexity string

const (
	ComplexitySimple  MessageComplexity = "simple"
	ComplexityComplex MessageComplexity = "complex"
)
