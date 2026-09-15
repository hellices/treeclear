//go:build !darwin

package process

import "testing"

func TestNativeSourcePreservesGopsutilOnOtherPlatforms(test *testing.T) {
	if _, retained := NativeSource().(GopsutilSource); !retained {
		test.Fatalf("native source = %T, want GopsutilSource", NativeSource())
	}
}
