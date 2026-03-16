package apierror

var (
	ErrInvalidRequestBody    = NewCustomError(400, "REQUEST_2001", "Invalid request body")
	ErrInvalidRequestFormat  = NewCustomError(400, "REQUEST_2004", "Invalid request body format")
	ErrValidationFailed      = NewCustomError(400, "VALIDATION_2001", "Validation failed")
	ErrMissingRequiredFields = NewCustomError(400, "REQUIRED_FIELDS_4001", "Missing required fields")
)

var (
	ErrHTTPBadRequest     = NewCustomError(400, "HTTP_400", "Bad Request")
	ErrHTTPUnauthorized   = NewCustomError(401, "HTTP_401", "Unauthorized")
	ErrHTTPForbidden      = NewCustomError(403, "HTTP_403", "Forbidden")
	ErrHTTPNotFound       = NewCustomError(404, "HTTP_404", "Not Found")
	ErrHTTPConflict       = NewCustomError(409, "HTTP_409", "Conflict")
	ErrHTTPInternalServer = NewCustomError(500, "HTTP_500", "Internal Server Error")
)
