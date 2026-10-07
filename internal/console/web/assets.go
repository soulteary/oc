// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// Package web contains the local console's self-contained browser assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js style.css
var assets embed.FS

// FS returns the console assets with index.html at the filesystem root.
func FS() fs.FS { return assets }
