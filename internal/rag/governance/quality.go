package governance

type QualityReport struct {
	Valid    bool
	Score    float64
	Errors   []string
	Warnings []string
}
