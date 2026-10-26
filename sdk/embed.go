// Package sdk embeds the frontend SDK served to every widget under /dw/.
package sdk

import "embed"

//go:embed js
var Files embed.FS
