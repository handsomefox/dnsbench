package main

import "os"

// Termux inserts the program's path into its arguments. See dropLinkerArg.
func init() {
	os.Args = dropLinkerArg(os.Args)
}
