package apierror

var (
	ErrSessionNotFound = NewCustomError(404, "SESSION_4001", "Session not found")
	ErrSessionExpired  = NewCustomError(410, "SESSION_4002", "Session has expired")
	ErrCatalogNotReady      = NewCustomError(422, "SESSION_4005", "Catalog snapshot not available, run discovery first")
	ErrCatalogPending       = NewCustomError(409, "SESSION_4006", "Discovery already in progress")
	ErrRunAlreadyActive     = NewCustomError(409, "SESSION_4007", "A run is already active for this session")
	ErrRunNotFound          = NewCustomError(404, "SESSION_4008", "Run not found")
	ErrNoActiveRun          = NewCustomError(404, "SESSION_4009", "No active run to stop")
)
