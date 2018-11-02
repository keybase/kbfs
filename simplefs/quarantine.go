// +build !darwin,!windows ios

package simplefs

import "golang.org/x/net/context"

func Quarantine(ctx context.Context, path string) error {
	return nil
}
