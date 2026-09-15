//go:build !darwin

package process

func NativeSource() Source {
	return GopsutilSource{}
}
