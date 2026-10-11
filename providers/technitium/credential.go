package technitium

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// requestError preserves the original cause for errors.Is/As while exposing
// only the credential-safe message to logs and other error formatting.
type requestError struct {
	message string
	cause   error
}

func (e *requestError) Error() string { return e.message }
func (e *requestError) Unwrap() error { return e.cause }

func redactRequestError(err error, token string) error {
	if err == nil {
		return nil
	}

	message := err.Error()
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		if requestURL, parseErr := url.Parse(requestErr.URL); parseErr == nil {
			requestURL.RawQuery = ""
			requestURL.ForceQuery = false
			requestURL.User = nil
			// url.Error quotes its URL; handle both that form and an outer
			// error that includes the URL without quotes.
			message = strings.ReplaceAll(message, fmt.Sprintf("%q", requestErr.URL), fmt.Sprintf("%q", requestURL.String()))
			message = strings.ReplaceAll(message, requestErr.URL, requestURL.String())
		}
	}
	if token != "" {
		// API/proxy error bodies and malformed URLs can also echo the token.
		encodedToken, _ := json.Marshal(token) // A string cannot fail JSON encoding.
		message = strings.ReplaceAll(message, string(encodedToken[1:len(encodedToken)-1]), "REDACTED")
		message = strings.ReplaceAll(message, url.QueryEscape(token), "REDACTED")
		message = strings.ReplaceAll(message, token, "REDACTED")
	}
	if message == err.Error() {
		return err
	}
	return &requestError{message: message, cause: err}
}
