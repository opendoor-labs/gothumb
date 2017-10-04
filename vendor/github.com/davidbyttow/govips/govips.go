// Package vips provides a library for transforming images that is built on lipvips. Libvips
// is an extremely fast C-library. Therefore, govips requires that libvips 8+ be installed
// and available on the target environment.
package vips

//go:generate scripts/codegen.sh

// #cgo pkg-config: vips
// #include "vips/vips.h"
import "C"
import (
	"fmt"
	"runtime"
	"sync"
)

// TODO(d): Tune these. Concurrency is set to a safe level but assumes
// openslide is not enabled.
const (
	defaultConcurrencyLevel = 10
	defaultMaxCacheFiles    = 500
	defaultMaxCacheMem      = 100 * 1024 * 1024
	defaultMaxCacheSize     = 1000
)

// VipsVersion if the primary version of libvips
const VipsVersion = string(C.VIPS_VERSION)

// VipsMajorVersion is the major version of libvips
const VipsMajorVersion = int(C.VIPS_MAJOR_VERSION)

// VipsMinorVersion if the minor vesrion of libvips
const VipsMinorVersion = int(C.VIPS_MINOR_VERSION)

var (
	running  = false
	initLock sync.Mutex
)

// Config allows fine-tuning of libvips library
type Config struct {
	ConcurrencyLevel int
	MaxCacheFiles    int
	MaxCacheMem      int
	MaxCacheSize     int
	ReportLeaks      bool
}

// Startup sets up the libvips support and ensures the versions are correct. Pass in nil for
// default configuration.
func Startup(config *Config) {
	initLock.Lock()
	runtime.LockOSThread()
	defer initLock.Unlock()
	defer runtime.UnlockOSThread()

	if running {
		debug("warning libvips already started")
		return
	}

	if C.VIPS_MAJOR_VERSION < 8 {
		panic("Requires libvips version 8+")
	}

	cName := C.CString("govips")
	defer freeCString(cName)

	err := C.vips_init(cName)
	if err != 0 {
		panic(fmt.Sprintf("Failed to start vips code=%d", err))
	}

	running = true

	C.vips_concurrency_set(defaultConcurrencyLevel)
	C.vips_cache_set_max(defaultMaxCacheSize)
	C.vips_cache_set_max_mem(defaultMaxCacheMem)
	C.vips_cache_set_max_files(defaultMaxCacheFiles)

	if config != nil {
		C.vips_leak_set(toGboolean(config.ReportLeaks))

		if config.ConcurrencyLevel > 0 {
			C.vips_concurrency_set(C.int(config.ConcurrencyLevel))
		}
		if config.MaxCacheFiles > 0 {
			C.vips_cache_set_max_files(C.int(config.MaxCacheFiles))
		}
		if config.MaxCacheMem > 0 {
			C.vips_cache_set_max_mem(C.size_t(config.MaxCacheMem))
		}
		if config.MaxCacheSize > 0 {
			C.vips_cache_set_max(C.int(config.MaxCacheSize))
		}
	}

	initTypes()
}

// PrintObjectReport outputs all of the current internal objects in libvips
func PrintObjectReport(label string) {
	fmt.Printf("\n=======================================\nMemory leaks: %s...\n", label)
	C.vips_object_print_all()
	fmt.Printf("=======================================\n\n")
}

func startupIfNeeded() {
	if !running {
		debug("libvips was forcibly started automatically, consider calling Startup/Shutdown yourself")
		Startup(nil)
	}
}

// Shutdown libvips
func Shutdown() {
	initLock.Lock()
	runtime.LockOSThread()
	defer initLock.Unlock()
	defer runtime.UnlockOSThread()

	if !running {
		debug("warning libvips not started")
		return
	}

	C.vips_shutdown()
	running = false
}

// ShutdownThread clears the cache for for the given thread
func ShutdownThread() {
	C.vips_thread_shutdown()
}
