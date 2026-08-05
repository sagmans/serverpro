package filedescriptor

import (
	"fmt"
	"math"
	"os"
)

func Int(file *os.File) (int, error) {
	fd := file.Fd()
	if fd > uintptr(math.MaxInt) {
		return 0, fmt.Errorf("file descriptor out of range: %d", fd)
	}
	return int(fd), nil
}
