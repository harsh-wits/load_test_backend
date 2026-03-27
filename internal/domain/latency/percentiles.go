package latency

import (
	"math"
	"sort"
)

// percentilesMs must be sorted ascending.
func percentileMs(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ComputeP50P95MaxFromSuccessLatenciesMs uses only successful callback samples (same basis as summary percentiles).
func ComputeP50P95MaxFromSuccessLatenciesMs(successLatenciesMs []int64) (p50, p95, max int64) {
	if len(successLatenciesMs) == 0 {
		return 0, 0, 0
	}
	sorted := make([]int64, len(successLatenciesMs))
	copy(sorted, successLatenciesMs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return percentileMs(sorted, 0.50), percentileMs(sorted, 0.95), sorted[len(sorted)-1]
}

func ComputeSummaryFromSuccessLatenciesMs(successLatenciesMs []int64) (avgMs float64, p90Ms, p95Ms, p99Ms int64) {
	if len(successLatenciesMs) == 0 {
		return 0, 0, 0, 0
	}

	var sum int64
	for _, v := range successLatenciesMs {
		sum += v
	}
	avgMs = float64(sum) / float64(len(successLatenciesMs))
	avgMs = math.Round(avgMs*100) / 100

	sorted := make([]int64, len(successLatenciesMs))
	copy(sorted, successLatenciesMs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	p90Ms = percentileMs(sorted, 0.90)
	p95Ms = percentileMs(sorted, 0.95)
	p99Ms = percentileMs(sorted, 0.99)
	return avgMs, p90Ms, p95Ms, p99Ms
}

