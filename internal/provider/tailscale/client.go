package tailscale

import (
	"net/http"

	"github.com/sagmans/serverpro/internal/poll"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
)

type Client struct {
	api     httpjson.Client
	tailnet string
	wait    poll.WaitFunc
}

func New(token, tailnet string) Client {
	return Client{api: httpjson.Client{BaseURL: "https://api.tailscale.com/api/v2", Token: token}, tailnet: tailnet}
}

func NewWithHTTP(token, tailnet, baseURL string, h *http.Client) Client {
	return Client{api: httpjson.Client{BaseURL: baseURL, Token: token, HTTP: h}, tailnet: tailnet}
}
