package utils

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const maxRetries = 3

var retryDelayUnit = time.Second

func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode >= 500
}

func DoRequestWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for i := range maxRetries {
		if i > 0 {
			if resp != nil {
				resp.Body.Close()
			}
			// Reset the request body for retries so the same body can be re-sent.
			// req.GetBody is set automatically by http.NewRequest when the body is
			// a bytes.Buffer, bytes.Reader, or strings.Reader. For nil bodies (GET,
			// HEAD) this branch is a no-op.
			if req.GetBody != nil {
				newBody, bodyErr := req.GetBody()
				if bodyErr != nil {
					return nil, fmt.Errorf("failed to reset request body for retry: %w", bodyErr)
				}
				req.Body = newBody
			}
		}

		resp, err = client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				break
			}
			if !shouldRetry(resp.StatusCode) {
				break
			}
		}

		if i < maxRetries-1 {
			if err = sleepWithCtx(req.Context(), retryDelayUnit*time.Duration(i+1)); err != nil {
				if resp != nil {
					resp.Body.Close()
				}
				return nil, fmt.Errorf("failed to sleep: %w", err)
			}
		}
	}
	return resp, err
}

func sleepWithCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
