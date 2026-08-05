package passwordhash

import (
	"regexp"
	"strconv"
)

var sha512Pattern = regexp.MustCompile(`^\$6\$rounds=([1-9][0-9]*)\$[./0-9A-Za-z]{16}\$[./0-9A-Za-z]{86}$`)

func ValidSHA512(hash string) bool {
	m := sha512Pattern.FindStringSubmatch(hash)
	if m == nil {
		return false
	}
	rounds, err := strconv.Atoi(m[1])
	return err == nil && rounds >= Rounds
}
