// Package errors - http 처리 상에 발생한 오류 관련 기능 제공 프키지
package errors

import (
	"fmt"
	"io"
	"net/http"
	"runtime/debug"

	"github.com/cloud-barista/cb-apigw/restapigw/pkg/logging"
	"github.com/cloud-barista/cb-apigw/restapigw/pkg/observability"
	"github.com/cloud-barista/cb-apigw/restapigw/pkg/render"
)

// ===== [ Constants and Variables ] =====

var (
	// ErrRouteNotFound happens when no route was matched
	ErrRouteNotFound = NewWithCode(http.StatusNotFound, "no API found with those values")
	// ErrInvalidID represents an invalid identifier
	ErrInvalidID = NewWithCode(http.StatusBadRequest, "please provide a valid ID")
)

// ===== [ Types ] =====

type (
	// fundamental - Message와 Stack 정보 관리 형식
	fundamental struct {
		msg string
		*stack
	}

	withStack struct {
		error
		*stack
	}

	withMessage struct {
		cause error
		msg   string
	}

	// Error - error 인터페이스가 적용된 사용자 정의 오류 형식
	Error struct {
		Code    int    `json:"-"`
		Message string `json:"error"`
	}
)

// ===== [ Implementations ] =====

// Error - 오류 정보에서 메시지 반환
func (e *Error) Error() string {
	return e.Message
}

func (f *fundamental) Error() string { return f.msg }

func (f *fundamental) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			io.WriteString(s, f.msg)
			f.stack.Format(s, verb)
			return
		}
		fallthrough
	case 's':
		io.WriteString(s, f.msg)
	case 'q':
		fmt.Fprintf(s, "%q", f.msg)
	}
}

func (w *withStack) Cause() error { return w.error }

func (w *withStack) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			fmt.Fprintf(s, "%+v", w.Cause())
			w.stack.Format(s, verb)
			return
		}
		fallthrough
	case 's':
		io.WriteString(s, w.Error())
	case 'q':
		fmt.Fprintf(s, "%q", w.Error())
	}
}

func (w *withMessage) Error() string { return w.msg + ": " + w.cause.Error() }
func (w *withMessage) Cause() error  { return w.cause }

func (w *withMessage) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			fmt.Fprintf(s, "%+v\n", w.Cause())
			io.WriteString(s, w.msg)
			return
		}
		fallthrough
	case 's', 'q':
		io.WriteString(s, w.Error())
	}
}

// ===== [ Private Functions ] =====
// ===== [ Public Functions ] =====

// New - 지정한 메시지를 기준으로 오류 정보 생성
func New(message string) error {
	return &fundamental{
		msg:   message,
		stack: callers(),
	}
}

// NewWithCode - 지정한 상태 코드와 메시지를 기준으로 오류정보 생성
func NewWithCode(code int, message string) *Error {
	return &Error{code, message}
}

// Handler - 오류 정보를 JSON 형식으로 변환
func Handler(rw http.ResponseWriter, req *http.Request, err interface{}) {
	logger := logging.GetLogger()
	logger.WithField("request-id", observability.RequestIDFromContext(req.Context()))

	switch internalErr := err.(type) {
	case *Error:
		logger.SetFields(logging.Fields{"code": internalErr.Code, "error": internalErr.Error()}).Info("Internal error handled")
		render.JSON(rw, internalErr.Code, internalErr)
	case error:
		logger.WithError(internalErr).WithField("stack", string(debug.Stack())).Error("Internal server error handled")
		render.JSON(rw, http.StatusInternalServerError, internalErr.Error())
	default:
		logger.WithField("error", err).WithField("stack", string(debug.Stack())).Error("Internal server error handled")
		render.JSON(rw, http.StatusInternalServerError, err)
	}
}

// NotFound - 조건에 맞는 Router 정보가 없는 경우 오류
func NotFound(rw http.ResponseWriter, req *http.Request) {
	Handler(rw, req, ErrRouteNotFound)
}

// RecoveryHandler - Panic이 발생했을 때 처리를 위한 Recovery Handler 반환
func RecoveryHandler(rw http.ResponseWriter, req *http.Request, err interface{}) {
	Handler(rw, req, err)
}

// Wrap - Stack Trace 정보들을 추가 설정한 오류 구성
// Wrap returns an error annotating err with a stack trace
// at the point Wrap is called, and the supplied message.
// If err is nil, Wrap returns nil.
func Wrap(err error, message string) error {
	if nil == err {
		return nil
	}
	err = &withMessage{
		cause: err,
		msg:   message,
	}
	return &withStack{
		err,
		callers(),
	}
}

// Wrapf returns an error annotating err with a stack trace
// at the point Wrapf is call, and the format specifier.
// If err is nil, Wrapf returns nil.
func Wrapf(err error, format string, args ...interface{}) error {
	if nil == err {
		return nil
	}
	err = &withMessage{
		cause: err,
		msg:   fmt.Sprintf(format, args...),
	}
	return &withStack{
		err,
		callers(),
	}
}

// WithStack annotates err with a stack trace at the point WithStack was called.
// If err is nil, WithStack returns nil.
func WithStack(err error) error {
	if nil == err {
		return nil
	}
	return &withStack{
		err,
		callers(),
	}
}

// WithMessage annotates err with a new message.
// If err is nil, WithMessage returns nil.
func WithMessage(err error, message string) error {
	if nil == err {
		return nil
	}
	return &withMessage{
		cause: err,
		msg:   message,
	}
}

// Cause returns the underlying cause of the error, if possible.
// An error value has a cause if it implements the following
// interface:
//
//     type causer interface {
//            Cause() error
//     }
//
// If the error does not implement Cause, the original error will
// be returned. If the error is nil, nil will be returned without further
// investigation.
func Cause(err error) error {
	type causer interface {
		Cause() error
	}

	for nil != err {
		cause, ok := err.(causer)
		if !ok {
			break
		}
		err = cause.Cause()
	}
	return err
}

// Errorf formats according to a format specifier and returns the string
// as a value that satisfies error.
// Errorf also records the stack trace at the point it was called.
func Errorf(format string, args ...interface{}) error {
	return &fundamental{
		msg:   fmt.Sprintf(format, args...),
		stack: callers(),
	}
}
