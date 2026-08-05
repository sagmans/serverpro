package passwordhash

import (
	"crypto/rand"
	"math/big"
	"strings"
)

func randomSalt(n int) (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[v.Int64()])
	}
	return b.String(), nil
}

func repeatToLength(src []byte, n int) []byte {
	if n == 0 {
		return nil
	}
	out := make([]byte, 0, n)
	for len(out) < n {
		want := n - len(out)
		if want > len(src) {
			want = len(src)
		}
		out = append(out, src[:want]...)
	}
	return out
}

func cryptBase64(b2, b1, b0 byte, n int) string {
	w := uint(b2)<<16 | uint(b1)<<8 | uint(b0)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(alphabet[w&0x3f])
		w >>= 6
	}
	return b.String()
}
