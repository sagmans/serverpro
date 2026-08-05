package passwordhash

import (
	"crypto/sha512"
	"fmt"
	"strings"
)

const (
	Rounds     = 100000
	saltLength = 16
	prefix     = "$6$"
	alphabet   = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

func GenerateSHA512(password string) (string, error) {
	salt, err := randomSalt(saltLength)
	if err != nil {
		return "", err
	}
	return SHA512Crypt(password, salt, Rounds)
}

func SHA512Crypt(password, salt string, rounds int) (string, error) {
	if rounds < 1000 || rounds > 999999999 {
		return "", fmt.Errorf("sha512 crypt rounds out of range")
	}
	if salt == "" {
		return "", fmt.Errorf("sha512 crypt salt required")
	}
	if len(salt) > saltLength {
		salt = salt[:saltLength]
	}
	for i := 0; i < len(salt); i++ {
		if !strings.ContainsRune(alphabet, rune(salt[i])) {
			return "", fmt.Errorf("sha512 crypt salt contains invalid character")
		}
	}

	keyBytes := []byte(password)
	saltBytes := []byte(salt)
	keyLen := len(keyBytes)
	saltLen := len(saltBytes)

	sumBBytes := sha512.Sum512(append(append(append([]byte{}, keyBytes...), saltBytes...), keyBytes...))
	sumB := sumBBytes[:]

	h := sha512.New()
	h.Write(keyBytes)
	h.Write(saltBytes)
	h.Write(repeatToLength(sumB, keyLen))
	for i := keyLen; i > 0; i >>= 1 {
		if i%2 == 0 {
			h.Write(keyBytes)
		} else {
			h.Write(sumB)
		}
	}
	sumA := h.Sum(nil)

	h.Reset()
	for i := 0; i < keyLen; i++ {
		h.Write(keyBytes)
	}
	seqP := repeatToLength(h.Sum(nil), keyLen)

	h.Reset()
	for i := 0; i < 16+int(sumA[0]); i++ {
		h.Write(saltBytes)
	}
	seqS := repeatToLength(h.Sum(nil), saltLen)

	for i := 0; i < rounds; i++ {
		h.Reset()
		if i%2 != 0 {
			h.Write(seqP)
		} else {
			h.Write(sumA)
		}
		if i%3 != 0 {
			h.Write(seqS)
		}
		if i%7 != 0 {
			h.Write(seqP)
		}
		if i%2 != 0 {
			h.Write(sumA)
		} else {
			h.Write(seqP)
		}
		sumA = h.Sum(sumA[:0])
	}

	var b strings.Builder
	b.WriteString(prefix)
	if rounds != 5000 {
		_, _ = fmt.Fprintf(&b, "rounds=%d$", rounds)
	}
	b.WriteString(salt)
	b.WriteByte('$')
	b.WriteString(cryptBase64(sumA[0], sumA[21], sumA[42], 4))
	b.WriteString(cryptBase64(sumA[22], sumA[43], sumA[1], 4))
	b.WriteString(cryptBase64(sumA[44], sumA[2], sumA[23], 4))
	b.WriteString(cryptBase64(sumA[3], sumA[24], sumA[45], 4))
	b.WriteString(cryptBase64(sumA[25], sumA[46], sumA[4], 4))
	b.WriteString(cryptBase64(sumA[47], sumA[5], sumA[26], 4))
	b.WriteString(cryptBase64(sumA[6], sumA[27], sumA[48], 4))
	b.WriteString(cryptBase64(sumA[28], sumA[49], sumA[7], 4))
	b.WriteString(cryptBase64(sumA[50], sumA[8], sumA[29], 4))
	b.WriteString(cryptBase64(sumA[9], sumA[30], sumA[51], 4))
	b.WriteString(cryptBase64(sumA[31], sumA[52], sumA[10], 4))
	b.WriteString(cryptBase64(sumA[53], sumA[11], sumA[32], 4))
	b.WriteString(cryptBase64(sumA[12], sumA[33], sumA[54], 4))
	b.WriteString(cryptBase64(sumA[34], sumA[55], sumA[13], 4))
	b.WriteString(cryptBase64(sumA[56], sumA[14], sumA[35], 4))
	b.WriteString(cryptBase64(sumA[15], sumA[36], sumA[57], 4))
	b.WriteString(cryptBase64(sumA[37], sumA[58], sumA[16], 4))
	b.WriteString(cryptBase64(sumA[59], sumA[17], sumA[38], 4))
	b.WriteString(cryptBase64(sumA[18], sumA[39], sumA[60], 4))
	b.WriteString(cryptBase64(sumA[40], sumA[61], sumA[19], 4))
	b.WriteString(cryptBase64(sumA[62], sumA[20], sumA[41], 4))
	b.WriteString(cryptBase64(0, 0, sumA[63], 2))
	return b.String(), nil
}
