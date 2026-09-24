//go:build !dev

package main

import (
	"context"
	"errors"
)

// runDev is absent from release builds: the development commands reset
// the database they point at, and a binary an operator runs must not
// carry that. Build with `-tags dev` to compile them in.
func runDev(context.Context, []string) error {
	return errors.New("the dev commands are not in this build; build with -tags dev")
}
