// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode/utf8"
)

// maxJSONDepth bounds the nesting a contract file may have; the model is four levels deep.
const maxJSONDepth = 32

// ParseJSON decodes the JSON form of a contract with encoding/json, refusing duplicate object
// keys, a byte order mark, invalid UTF-8 and trailing data. Objects become map[string]any,
// arrays []any and integral numbers int64, so the document has the shape ParseTOML returns.
func ParseJSON(data []byte) (any, []Finding) {
	fail := func(pointer, message string) (any, []Finding) {
		return nil, []Finding{errorFinding("profile-violation", pointer, message)}
	}
	if len(data) > MaxContractBytes {
		return nil, []Finding{errorFinding("limit-exceeded", "", fmt.Sprintf("contract file exceeds the %d byte limit", MaxContractBytes))}
	}
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return fail("", "a byte order mark is not allowed")
	}
	if !utf8.Valid(data) {
		return fail("", "the file is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := decodeJSONValue(dec, "", 0)
	if err != nil {
		var dup duplicateKey
		if errors.As(err, &dup) {
			return fail(dup.pointer, "duplicate key "+strconv.Quote(dup.key))
		}
		return fail("", "invalid JSON: "+err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fail("", "invalid JSON: data after the top-level value")
	}
	return value, nil
}

type duplicateKey struct{ pointer, key string }

func (d duplicateKey) Error() string { return "duplicate key " + d.key }

func decodeJSONValue(dec *json.Decoder, pointer string, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, errors.New("nesting is too deep")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			object := map[string]any{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key := keyToken.(string)
				child := pointerJoin(pointer, key)
				if _, dup := object[key]; dup {
					return nil, duplicateKey{child, key}
				}
				value, err := decodeJSONValue(dec, child, depth+1)
				if err != nil {
					return nil, err
				}
				object[key] = value
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return object, nil
		case '[':
			array := []any{}
			for dec.More() {
				value, err := decodeJSONValue(dec, fmt.Sprintf("%s/%d", pointer, len(array)), depth+1)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return array, nil
		}
		return nil, errors.New("unexpected delimiter")
	case json.Number:
		if n, err := strconv.ParseInt(string(t), 10, 64); err == nil {
			return n, nil
		}
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			return nil, err
		}
		if f == math.Trunc(f) && math.Abs(f) < 1e15 {
			return int64(f), nil
		}
		return f, nil
	}
	return token, nil
}
