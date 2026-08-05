package passwordhash

import (
	"strings"
	"testing"
)

func TestSHA512CryptKnownVector(t *testing.T) {
	got, err := SHA512Crypt("Hello world!", "saltstring", 5000)
	if err != nil {
		t.Fatal(err)
	}
	want := "$6$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJuesI68u4OTLiBFdcbYEdFCoEOfaS35inz1"
	if got != want {
		t.Fatalf("hash mismatch\nwant %s\n got %s", want, got)
	}
}

func TestGenerateSHA512UsesRoundsAndSalt(t *testing.T) {
	hash, err := GenerateSHA512("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$6$rounds=100000$") {
		t.Fatalf("hash prefix = %q", hash)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 5 || len(parts[3]) != 16 || len(parts[4]) != 86 {
		t.Fatalf("bad sha512 crypt shape: %q parts=%#v", hash, parts)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatalf("hash leaked plaintext: %q", hash)
	}
}

func TestValidSHA512RejectsMalformedHash(t *testing.T) {
	for _, hash := range []string{"", "$5$rounds=100000$abcdefghijklmnop$abcd", "$6$rounds=999$abcdefghijklmnop$abcd", "$6$rounds=100000$short$abcd"} {
		if ValidSHA512(hash) {
			t.Fatalf("accepted malformed hash %q", hash)
		}
	}
}
