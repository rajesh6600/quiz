package errors

// Error codes for standardized error responses
const (
	// Authentication errors
	ErrCodeUnauthorized      = "unauthorized"
	ErrCodeForbidden         = "forbidden"
	ErrCodeInvalidToken      = "invalid_token"
	ErrCodeTokenExpired      = "token_expired"
	ErrCodeAuthenticationRequired = "authentication_required"

	// Validation errors
	ErrCodeInvalidRequest    = "invalid_request"
	ErrCodeValidationFailed  = "validation_failed"
	ErrCodeMissingField      = "missing_field"

	// Resource errors
	ErrCodeNotFound          = "not_found"
	ErrCodeAlreadyExists     = "already_exists"
	ErrCodeConflict          = "conflict"

	// Business logic errors
	ErrCodeRegistrationFailed = "registration_failed"
	ErrCodeLoginFailed        = "login_failed"
	ErrCodeGuestCreationFailed = "guest_creation_failed"
	ErrCodeConversionFailed   = "conversion_failed"
	ErrCodeRefreshFailed      = "refresh_failed"
	ErrCodeSetUsernameFailed  = "set_username_failed"
	ErrCodeResetFailed        = "reset_failed"
	ErrCodeUsernameTaken      = "username_taken"

	// Room/Match errors
	ErrCodeRoomCreationFailed = "room_creation_failed"
	ErrCodeRoomNotFound       = "room_not_found"
	ErrCodeRoomFetchFailed    = "room_fetch_failed"
	ErrCodeGuestsCannotCreateRooms = "guests_cannot_create_rooms"
	ErrCodeInvalidRoomCode    = "invalid_room_code"
	ErrCodeJoinFailed         = "join_failed"
	ErrCodeRoomStartFailed    = "room_start_failed"
	ErrCodeMatchCreationFailed = "match_creation_failed"
	ErrCodeInvalidMatchID     = "invalid_match_id"
	ErrCodeSubmitFailed       = "submit_failed"

	// Queue errors
	ErrCodeEnqueueFailed      = "enqueue_failed"
	ErrCodeInvalidQueueToken  = "invalid_queue_token"
	ErrCodeQueueTokenNotFound = "queue_token_not_found"

	// WebSocket errors
	ErrCodeInvalidPayload     = "invalid_payload"
	ErrCodeUnknownMessageType  = "unknown_message_type"
	ErrCodeConnectionError     = "connection_error"

	// Server errors
	ErrCodeInternalError      = "internal_error"
	ErrCodeServiceUnavailable = "service_unavailable"
	ErrCodeUpstreamError      = "upstream_error"

	// Feature availability
	ErrCodeFeatureNotAvailable = "feature_not_available"
	ErrCodeNotImplemented      = "not_implemented"

	// OAuth errors
	ErrCodeOAuthNotConfigured  = "oauth_not_configured"
	ErrCodeOAuthStartFailed     = "oauth_start_failed"
	ErrCodeOAuthCallbackFailed = "oauth_callback_failed"
	ErrCodeOAuthMissingCode     = "missing_code"
	ErrCodeOAuthInvalidState    = "invalid_state"
	ErrCodeUserCreationFailed   = "user_creation_failed"

	// Leaderboard errors
	ErrCodeLeaderboardFetchFailed = "leaderboard_fetch_failed"
	ErrCodeUnknownWindow          = "unknown_leaderboard_window"
)

