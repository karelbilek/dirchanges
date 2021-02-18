// +build !windows

package dirchanges

import (
	"path/filepath"
	"strings"
)

func isHiddenFile(path string) (bool, error) {
	return strings.HasPrefix(filepath.Base(path), "."), nil
}
