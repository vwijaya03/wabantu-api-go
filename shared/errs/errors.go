package errs

import "encore.dev/beta/errs"

func NotFound(msg string) error {
	return &errs.Error{Code: errs.NotFound, Message: msg}
}

func BadRequest(msg string) error {
	return &errs.Error{Code: errs.InvalidArgument, Message: msg}
}

func Forbidden(msg string) error {
	return &errs.Error{Code: errs.PermissionDenied, Message: msg}
}

func Unauthenticated(msg string) error {
	return &errs.Error{Code: errs.Unauthenticated, Message: msg}
}

func Internal(msg string) error {
	return &errs.Error{Code: errs.Internal, Message: msg}
}

func Unavailable(msg string) error {
	return &errs.Error{Code: errs.Unavailable, Message: msg}
}

func AlreadyExists(msg string) error {
	return &errs.Error{Code: errs.AlreadyExists, Message: msg}
}
