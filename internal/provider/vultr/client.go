package vultr

import (
	"net/http"

	"github.com/sagmans/serverpro/internal/provider/httpjson"
)

type Client struct{ api httpjson.Client }

func New(token string) Client {
	return Client{api: httpjson.Client{BaseURL: "https://api.vultr.com/v2", Token: token}}
}

func NewWithHTTP(token, baseURL string, h *http.Client) Client {
	return Client{api: httpjson.Client{BaseURL: baseURL, Token: token, HTTP: h}}
}
