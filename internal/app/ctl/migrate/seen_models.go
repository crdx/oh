package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/format"
)

const (
	seenModelsInitialFormat = 1
	seenModelsWireFormat    = 2
)

type SeenModelsOptions struct {
	Path   string
	DryRun bool
}

type seenModelsStep func(data []byte) ([]byte, error)

var seenModelsSteps = map[int]seenModelsStep{
	seenModelsInitialFormat: wiresJoinSeenModels,
}

func MigrateSeenModels(options SeenModelsOptions) (int, bool, error) {
	data, err := os.ReadFile(options.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return model.SeenModelsFormat, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	fromFormat, err := format.ReadJSON(data)
	if err != nil {
		return 0, true, err
	}
	if err := format.Check(fromFormat, model.SeenModelsFormat); err != nil {
		return fromFormat, true, fmt.Errorf("%w: upgrade oh", err)
	}
	if fromFormat == model.SeenModelsFormat {
		return fromFormat, true, nil
	}

	migratedData := data
	for format := fromFormat; format < model.SeenModelsFormat; format++ {
		migrationStep, isFound := seenModelsSteps[format]
		if !isFound {
			return fromFormat, true, fmt.Errorf("nothing knows how to migrate seen models format %d", format)
		}
		migratedData, err = migrationStep(migratedData)
		if err != nil {
			return fromFormat, true, fmt.Errorf("format %d: %w", format, err)
		}
	}
	if options.DryRun {
		return fromFormat, true, nil
	}

	if err := keepFileCopy(seenModelsBackupPath(options.Path), data); err != nil {
		return fromFormat, true, err
	}
	if err := replaceFile(options.Path, migratedData); err != nil {
		return fromFormat, true, err
	}

	return fromFormat, true, nil
}

func seenModelsBackupPath(path string) string {
	return fmt.Sprintf("%s.pre-v%d", path, model.SeenModelsFormat)
}

func wiresJoinSeenModels(data []byte) ([]byte, error) {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("the seen models could not be read: %w", err)
	}

	var providers map[string][]map[string]json.RawMessage
	if raw, hasProviders := record["providers"]; hasProviders {
		if err := json.Unmarshal(raw, &providers); err != nil {
			return nil, fmt.Errorf("the seen providers could not be read: %w", err)
		}
	}

	for _, seenModel := range providers[historicalOpencodeGoProvider] {
		modelID, isNamed := historicalString(seenModel["id"])
		if _, hasWire := seenModel["wire"]; hasWire || !isNamed || modelID == "" {
			continue
		}
		setHistoricalString(seenModel, "wire", historicalWireFor(modelID))
	}

	if providers != nil {
		restatedProviders, err := json.Marshal(providers)
		if err != nil {
			return nil, err
		}
		record["providers"] = restatedProviders
	}
	record["version"] = json.RawMessage(strconv.Itoa(seenModelsWireFormat))

	return json.Marshal(record)
}
