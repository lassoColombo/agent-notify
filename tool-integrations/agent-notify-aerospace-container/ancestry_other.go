//go:build !darwin

package main

import "os"

// aerospace is a macOS window manager, so everywhere else this program is a
// container that captures an empty chain and places nothing. It still builds
// and its pure half is still tested, which is the whole reason this file is
// here rather than a build constraint on the package.
func Ancestry(int) []Ancestor { return nil }

func Self() int { return os.Getpid() }
