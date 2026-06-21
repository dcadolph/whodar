// Package cmd implements the whodar command-line interface.
package cmd

// Execute runs the whodar root command and returns any error to the caller.
func Execute() error {
	return newRootCmd().Execute()
}
