// Package runtimes embeds the default administrator-approved runtime definitions.
package runtimes

import "embed"

//go:embed *.yaml
var FS embed.FS
