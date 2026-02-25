//go:build debug

package main

// this file is included in the build when the "debug" tag is set,
// providing a debug version of the debug constant
// e.g go build -tags debug -o main-debug .
const debug = true
