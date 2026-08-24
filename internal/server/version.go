package server

import (
	"runtime/debug"
	"time"
)

// BaseVersion is the app version shown in the UI. When the binary is built
// from a git checkout, Go (1.18+) stamps VCS info into the binary and the
// displayed version becomes e.g. "1.0.3 (260824.90a309)".
const BaseVersion = "1.0.3"

// versionPlaceholder in web/index.html is replaced with appVersion when the
// page is served.
const versionPlaceholder = "__VERSION__"

// appVersion is computed once at startup; the build info never changes
// during the process lifetime.
var appVersion = fullVersion()

// fullVersion returns BaseVersion with the git commit date (yymmdd) and short
// hash appended, when available.
func fullVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return BaseVersion
	}
	var rev, vcsTime string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			vcsTime = s.Value
		}
	}
	if rev == "" {
		return BaseVersion
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if t, err := time.Parse(time.RFC3339, vcsTime); err == nil {
		return BaseVersion + " (" + t.Format("060102") + "." + rev + ")"
	}
	return BaseVersion + " (" + rev + ")"
}
