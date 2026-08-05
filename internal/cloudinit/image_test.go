package cloudinit

import (
	"testing"

	"github.com/assagman/serverpro/internal/compute"
)

func TestSupportsImageRequiresUbuntu2404(t *testing.T) {
	for _, tc := range []struct {
		name  string
		image compute.Image
		want  bool
	}{
		{name: "version metadata", image: compute.Image{OSFlavor: "ubuntu", OSVersion: "24.04"}, want: true},
		{name: "description metadata", image: compute.Image{OSFlavor: "Ubuntu", Description: "Ubuntu 24.04 LTS x64"}, want: true},
		{name: "debian", image: compute.Image{OSFlavor: "debian", OSVersion: "12"}},
		{name: "old ubuntu", image: compute.Image{OSFlavor: "ubuntu", OSVersion: "22.04", Description: "Ubuntu 22.04 LTS"}},
		{name: "windows", image: compute.Image{OSFlavor: "windows", Description: "Windows Server 2025"}},
		{name: "unknown version", image: compute.Image{OSFlavor: "ubuntu", Description: "Ubuntu current"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SupportsImage(tc.image); got != tc.want {
				t.Fatalf("SupportsImage(%+v) = %t, want %t", tc.image, got, tc.want)
			}
		})
	}
}
