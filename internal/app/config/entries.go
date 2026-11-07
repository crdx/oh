package config

import (
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

type entryTables map[string]reflect.Value

func namedEntries(config *Config) entryTables {
	tables := entryTables{}
	value := reflect.ValueOf(config).Elem()
	for index := range value.NumField() {
		field := value.Type().Field(index)
		table := value.Field(index)
		if !field.IsExported() || table.Kind() != reflect.Map || table.Type().Elem().Kind() != reflect.Struct {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
		tables[name] = table
	}
	return tables
}

func (self entryTables) clone() entryTables {
	clones := make(entryTables, len(self))
	for name, table := range self {
		if table.IsNil() {
			clones[name] = table
			continue
		}
		clone := reflect.MakeMapWithSize(table.Type(), table.Len())
		iterator := table.MapRange()
		for iterator.Next() {
			clone.SetMapIndex(iterator.Key(), iterator.Value())
		}
		clones[name] = clone
	}
	return clones
}

func mergeNamedEntries(config *Config, previousEntries entryTables, data []byte) error {
	var document map[string]toml.Primitive
	meta, err := toml.Decode(string(data), &document)
	if err != nil {
		return err
	}

	tables := namedEntries(config)
	for name, primitive := range document {
		previousTable, isNamedEntries := previousEntries[name]
		if !isNamedEntries || previousTable.IsNil() {
			continue
		}

		var entries map[string]toml.Primitive
		if err := meta.PrimitiveDecode(primitive, &entries); err != nil {
			return err
		}

		table := tables[name]
		for entryName, entryPrimitive := range entries {
			key := reflect.ValueOf(entryName).Convert(table.Type().Key())
			previous := previousTable.MapIndex(key)
			if !previous.IsValid() {
				continue
			}

			entry := reflect.New(table.Type().Elem())
			entry.Elem().Set(previous)
			if err := meta.PrimitiveDecode(entryPrimitive, entry.Interface()); err != nil {
				return err
			}
			table.SetMapIndex(key, entry.Elem())
		}
	}
	return nil
}
