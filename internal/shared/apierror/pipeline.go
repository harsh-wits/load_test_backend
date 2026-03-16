package apierror

var (
	ErrUpstreamTimeout    = NewCustomError(504, "PIPELINE_5001", "BPP did not respond in time")
	ErrUpstreamNonSuccess = NewCustomError(502, "PIPELINE_5002", "BPP returned a non-success status")
	ErrRateLimited        = NewCustomError(429, "PIPELINE_4001", "Rate limit exceeded")
	ErrOnSearchNotFound   = NewCustomError(404, "PIPELINE_4002", "on_search payload not found")
	ErrBatchBuildFailed   = NewCustomError(500, "PIPELINE_5003", "Failed to build payload batch")
)
