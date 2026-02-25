//go:build !debug

package main

// this file is included in the build when the "debug" tag is NOT set,
// providing a non-debug version of the debug constant
// e.g go build -o main .
const debug = false
