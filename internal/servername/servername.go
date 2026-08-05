package servername

const Default = "default"

func Normalize(server string) string {
	if server == "" {
		return Default
	}
	return server
}
