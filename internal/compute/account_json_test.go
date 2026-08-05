package compute

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestAccountTokenNeverMarshalsInRequestDTOs(t *testing.T) {
	const secret = "provider-token-sentinel"
	account := Account{Name: "prod", Provider: "hetzner", Token: secret, Scope: "demo/web"}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "account", value: account},
		{name: "catalog", value: CatalogQuery{Account: account}},
		{name: "list", value: ListServersQuery{Account: account}},
		{name: "create", value: CreateServerRequest{Account: account}},
		{name: "status", value: ServerRef{Account: account}},
		{name: "power", value: PowerRequest{Account: account}},
		{name: "delete", value: DeleteServerRequest{Account: account}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(body, []byte(secret)) {
				t.Fatalf("provider token leaked in JSON: %s", body)
			}
			if !bytes.Contains(body, []byte("prod")) {
				t.Fatalf("account identity missing from JSON: %s", body)
			}
		})
	}
}
