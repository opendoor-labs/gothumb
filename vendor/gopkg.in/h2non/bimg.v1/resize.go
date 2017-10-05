// +build go1.7
// must use files from vendor. resize.go and resize_legacy.go

package bimg

import (
	"runtime"
)

// Resize is used to transform a given image as byte buffer
// with the passed options.
func Resize(buf []byte, o Options) ([]byte, error) {
	// Required in order to prevent premature garbage collection. See:
	// https://github.com/h2non/bimg/pull/162
	defer runtime.KeepAlive(buf)
	return resizer(buf, o)
}
