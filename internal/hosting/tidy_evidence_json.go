// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"
)

// decodeTidyJSON prevents encoding/json's case-insensitive struct matching from
// accepting an absent exact proof member or merging an alias into its value.
// Unknown provider members remain data and are ignored; only members admitted
// by the destination type are matched and recursively checked.
func decodeTidyJSON(raw []byte, out any) error {
	kind := reflect.TypeOf(out)
	if kind == nil || kind.Kind() != reflect.Pointer || !utf8.Valid(raw) || len(raw) > maxBytes || uniqueReleaseJSON(raw) != nil {
		return errors.New("invalid tidy provider JSON")
	}
	if err := tidyExactJSONMembers(raw, kind.Elem()); err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

var tidyRawJSONType = reflect.TypeOf(json.RawMessage{})

func tidyExactJSONMembers(raw json.RawMessage, kind reflect.Type) error {
	for kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	// Raw repository values have their own typed decoder before use.
	if kind == tidyRawJSONType {
		return nil
	}
	switch kind.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		for key, value := range object {
			for i := 0; i < kind.NumField(); i++ {
				field := kind.Field(i)
				if field.PkgPath != "" {
					continue
				}
				name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
				if name == "-" {
					continue
				}
				if name == "" {
					name = field.Name
				}
				if !strings.EqualFold(key, name) {
					continue
				}
				if key != name {
					return errors.New("tidy provider member name is not exact")
				}
				if err := tidyExactJSONMembers(value, field.Type); err != nil {
					return err
				}
				break
			}
		}
	case reflect.Slice:
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			if err := tidyExactJSONMembers(row, kind.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
