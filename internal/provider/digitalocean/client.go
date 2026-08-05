package digitalocean

import (
	"net/http"

	"github.com/assagman/serverpro/internal/provider/httpjson"
)

type Client struct{ api httpjson.Client }

func New(token string) Client {
	return Client{api: httpjson.Client{BaseURL: "https://api.digitalocean.com/v2", Token: token}}
}

func NewWithHTTP(token, baseURL string, h *http.Client) Client {
	return Client{api: httpjson.Client{BaseURL: baseURL, Token: token, HTTP: h}}
}
