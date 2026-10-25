package location

import (
	"os"
	"path/filepath"

	"crdx.org/oh/internal/xdg"
)

const (
	namespace = "org.crdx"
	app       = "oh"

	StateDirVariable  = "OH_STATE_DIR"
	ConfigDirVariable = "OH_CONFIG_DIR"
)

func GetStateDir(parts ...string) string {
	return resolve(StateDirVariable, xdg.StatePath, parts)
}

func GetConfigDir(parts ...string) string {
	return resolve(ConfigDirVariable, xdg.ConfigPath, parts)
}

func resolve(variable string, fallback func(...string) string, parts []string) string {
	if root := os.Getenv(variable); filepath.IsAbs(root) {
		return filepath.Join(append([]string{root}, parts...)...)
	}

	return fallback(append([]string{namespace, app}, parts...)...)
}

func GetConfigFile() string {
	return GetConfigDir("config.toml")
}

func GetGlobalContextPath() string {
	return GetConfigDir("SYSTEM.md")
}

func GetDataDir(parts ...string) string {
	return GetStateDir(append([]string{"data"}, parts...)...)
}

func GetModelCachePath() string {
	return GetDataDir("models.json")
}

func GetSeenModelsPath() string {
	return GetDataDir("seen_models.json")
}

func GetUsageCachePath(provider string, isSimulation bool) string {
	if isSimulation {
		return ""
	}

	return GetStateDir("usage", provider+".json")
}

func GetAnalysisCachePath() string {
	return GetDataDir("analysis.json")
}

func GetExchangeRateCachePath() string {
	return GetDataDir("rates.json")
}

func GetModelRoundRobinPath() string {
	return GetDataDir("model-round-robin.json")
}

func GetSubagentRoundRobinPath() string {
	return GetDataDir("subagent-round-robin.json")
}

func GetHistoryPath() string {
	return GetDataDir("history")
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
