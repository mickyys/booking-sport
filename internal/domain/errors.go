package domain

type AppError struct {
	Message    string
	StatusCode int
}

func (e *AppError) Error() string {
	return e.Message
}

func NewConflictError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 409}
}

func NewBadRequestError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 400}
}

func NewNotFoundError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 404}
}
