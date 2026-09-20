package cli

import "runtime/debug"

func Version() string {
	return buildVersion(debug.ReadBuildInfo())
}

func buildVersion(info *debug.BuildInfo, isAvailable bool) string {
	if !isAvailable || info == nil || info.Main.Version == "" {
		return "unknown"
	}

	return info.Main.Version
}
