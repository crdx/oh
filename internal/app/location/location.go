package location

import (
	"os"
	"path/filepath"

	"crdx.org/io/internal/xdg"
)

const (
	namespace = "org.crdx"
	app       = "oh"

	StateDirVariable = "OH_STATE_DIR"
)

func GetStateDir(parts ...string) string {
	if root := os.Getenv(StateDirVariable); filepath.IsAbs(root) {
		return filepath.Join(append([]string{root}, parts...)...)
	}

	return xdg.StatePath(append([]string{namespace, app}, parts...)...)
}

func GetConfigDir(parts ...string) string {
	return xdg.ConfigPath(append([]string{namespace, app}, parts...)...)
}

func GetConfigFile() string {
	return GetConfigDir("config.toml")
}

func GetGlobalContextPath() string {
	return GetConfigDir("SYSTEM.md")
}

func GetModelCachePath(isSimulation bool) string {
	if isSimulation {
		return GetStateDir("models.sim.json")
	}

	return GetStateDir("models.json")
}

func GetUsageCachePath(provider string, isSimulation bool) string {
	if isSimulation {
		return ""
	}

	return GetStateDir("usage", provider+".json")
}

func GetAnalysisCachePath() string {
	return xdg.CachePath(namespace, app, "analysis.json")
}

func GetExchangeRateCachePath() string {
	return GetStateDir("rates.json")
}

func GetModelRoundRobinPath() string {
	return GetStateDir("model-round-robin.json")
}

func GetHistoryPath() string {
	return GetStateDir("history")
}

func GetSessionsDir() string {
	return GetStateDir("sessions")
}

func GetShellHomeDir() string {
	return GetStateDir("home")
}

func GetFarmDir() string {
	return GetStateDir("farm")
}

func GetTmpDir(name string) string {
	return filepath.Join(GetFarmDir(), name)
}
