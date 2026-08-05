package vultr

import "encoding/base64"

func encodeUserData(data string) string {
	return base64.StdEncoding.EncodeToString([]byte(data))
}
