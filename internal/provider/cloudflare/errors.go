package cloudflare

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/assagman/serverpro/internal/provider/httpjson"
)

const tunnelActiveConnectionsCode = 1022

func TunnelHasActiveConnections(err error) bool {
	var statusErr *httpjson.StatusError
	if !errors.As(err, &statusErr) || statusErr == nil || statusErr.StatusCode != http.StatusBadRequest {
		return false
	}
	var body struct {
		Errors []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal([]byte(statusErr.Body), &body) != nil {
		return false
	}
	for _, apiErr := range body.Errors {
		if apiErr.Code == tunnelActiveConnectionsCode && strings.Contains(strings.ToLower(apiErr.Message), "active connections") {
			return true
		}
	}
	return false
}
