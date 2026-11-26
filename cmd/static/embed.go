package static

import _ "embed"

// IndexHTML is the embedded single-file onboarding and admin dashboard UI.
//
//go:embed index.html
var IndexHTML []byte
