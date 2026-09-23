package main

import (
	"os"
	"path/filepath"
)

// dropLinkerArg undoes an argument that Termux adds on Android 10 and later.
// Android forbids running files from an app's data directory, so Termux
// starts programs through /system/bin/linker64, which inserts the
// program's absolute path as args[1]. Go's flag parser stops at that first
// non-flag, so every flag after it would be ignored.
//
// It removes args[1] only when it is an absolute path to the same file as
// args[0], which no real first argument can be. argv_android.go applies it,
// so only the Android build ever changes its arguments.
func dropLinkerArg(args []string) []string {
	if len(args) < 2 || !filepath.IsAbs(args[1]) || filepath.Base(args[1]) != filepath.Base(args[0]) {
		return args
	}
	self, err := os.Stat(args[0])
	if err != nil {
		return args
	}
	inserted, err := os.Stat(args[1])
	if err != nil || !os.SameFile(self, inserted) {
		return args
	}
	return append(args[:1:1], args[2:]...)
}
