//go:build !parakeet

package main

// parakeetSupported is false in the default (CGO-free) build.
const parakeetSupported = false
