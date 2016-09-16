// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "strings"

// PathType describes the types for different paths
type PathType string

const (
	// KeybasePathType is the keybase root (like /keybase)
	KeybasePathType PathType = "keybase"
	// PublicPathType is the keybase public (like /keybase/public)
	PublicPathType PathType = "public"
	// PrivatePathType is the keybase private (like /keybase/private)
	PrivatePathType PathType = "private"
)

// BuildCanonicalPath returns a canonical path for a path components.
// This a canonical path and may need to be converted to a platform
// specific path, for example, on Windows, this might correspond to
// k:\private\username.
func BuildCanonicalPath(pathType PathType, paths ...string) string {
	var prefix string
	switch pathType {
	case KeybasePathType:
		prefix = "/" + string(KeybasePathType)
	case PublicPathType:
		prefix = "/" + string(KeybasePathType) + "/" + string(PublicPathType)
	case PrivatePathType:
		prefix = "/" + string(KeybasePathType) + "/" + string(PrivatePathType)
	}
	pathElements := []string{prefix}
	for _, p := range paths {
		if p != "" {
			pathElements = append(pathElements, p)
		}
	}
	return strings.Join(pathElements, "/")
}
