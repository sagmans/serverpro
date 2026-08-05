package cloudinit

import (
	"encoding/json"
	"strings"
	"text/template"

	"github.com/sagmans/serverpro/internal/shell"
)

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"join":       strings.Join,
		"jsonString": jsonString,
		"shellQuote": shell.Quote,
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
