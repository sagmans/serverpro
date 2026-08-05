package hetzner

import (
	"net/http"

	"github.com/sagmans/serverpro/internal/provider/httpjson"
)

type Client struct{ api httpjson.Client }

func New(token string) Client {
	return Client{api: httpjson.Client{BaseURL: "https://api.hetzner.cloud/v1", Token: token}}
}

func NewWithHTTP(token, baseURL string, h *http.Client) Client {
	return Client{api: httpjson.Client{BaseURL: baseURL, Token: token, HTTP: h}}
}
