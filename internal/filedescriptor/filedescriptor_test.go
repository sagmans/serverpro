package filedescriptor

import (
	"os"
	"testing"
)

func TestIntReturnsFileDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "fd-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	fd, err := Int(file)
	if err != nil {
		t.Fatal(err)
	}
	if fd != int(file.Fd()) {
		t.Fatalf("fd = %d, want %d", fd, file.Fd())
	}
}
