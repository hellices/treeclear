package snapshot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

const maximumJSONValues = maximumAdministrativeEntries * 8

type manifestJSONBudget struct {
	remainingValues     int
	administrativeBytes int
}

func EncodeManifest(value Manifest) ([]byte, error) {
	if err := ValidateManifest(value); err != nil {
		return nil, err
	}
	value.CreatedAt = value.CreatedAt.UTC()
	value.AdministrativeEntries = slices.Clone(value.AdministrativeEntries)
	slices.SortFunc(value.AdministrativeEntries, func(left, right AdminEntry) int { return strings.Compare(left.Path, right.Path) })
	contents, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot encode manifest: %w", ErrManifestInvalid, err)
	}
	if len(contents) > maximumManifestBytes {
		return nil, manifestLimit("encoded document bytes")
	}
	return contents, nil
}

func DecodeManifest(contents []byte) (Manifest, error) {
	if len(contents) > maximumManifestBytes {
		return Manifest{}, manifestLimit("encoded document bytes")
	}
	if !utf8.Valid(contents) {
		return Manifest{}, fmt.Errorf("%w: invalid UTF-8", ErrManifestInvalid)
	}
	if err := checkManifestJSON(contents); err != nil {
		return Manifest{}, err
	}
	var value Manifest
	if err := json.Unmarshal(contents, &value); err != nil {
		return Manifest{}, fmt.Errorf("%w: invalid document: %w", ErrManifestInvalid, err)
	}
	canonical, err := EncodeManifest(value)
	if err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(canonical, contents) {
		return Manifest{}, fmt.Errorf("%w: noncanonical document", ErrManifestInvalid)
	}
	return value, nil
}

func checkManifestJSON(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return fmt.Errorf("%w: expected a JSON object", ErrManifestInvalid)
	}
	budget := manifestJSONBudget{remainingValues: maximumJSONValues}
	if err := checkJSONValue(decoder, first, 1, &budget); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing JSON", ErrManifestInvalid)
	}
	return nil
}

func checkJSONValue(decoder *json.Decoder, token json.Token, depth int, budget *manifestJSONBudget) error {
	if budget.remainingValues == 0 {
		return manifestLimit("aggregate JSON values")
	}
	budget.remainingValues--
	opening, collection := token.(json.Delim)
	if !collection {
		return nil
	}
	if depth > 8 {
		return manifestLimit("JSON nesting")
	}
	var closing json.Delim
	var maximumEntries int
	switch opening {
	case '{':
		closing, maximumEntries = '}', 32
	case '[':
		closing, maximumEntries = ']', maximumAdministrativeEntries
	default:
		return fmt.Errorf("%w: unexpected JSON delimiter", ErrManifestInvalid)
	}
	entries := 0
	for decoder.More() {
		if entries >= maximumEntries {
			return manifestLimit("JSON collection entries")
		}
		entries++
		dataField := false
		if opening == '{' {
			key, err := decoder.Token()
			name, text := key.(string)
			if err != nil || !text {
				return fmt.Errorf("%w: invalid JSON object key", ErrManifestInvalid)
			}
			dataField = strings.EqualFold(name, "data")
		}
		value, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: invalid JSON value: %w", ErrManifestInvalid, err)
		}
		if dataField {
			if err := checkAdministrativeDataSize(value, budget); err != nil {
				return err
			}
		}
		if err := checkJSONValue(decoder, value, depth+1, budget); err != nil {
			return err
		}
	}
	last, err := decoder.Token()
	if err != nil || last != closing {
		return fmt.Errorf("%w: unterminated JSON collection", ErrManifestInvalid)
	}
	return nil
}

func checkAdministrativeDataSize(value json.Token, budget *manifestJSONBudget) error {
	if value == nil {
		return nil
	}
	encoded, text := value.(string)
	if !text || len(encoded)%4 != 0 || strings.ContainsAny(encoded, "\r\n") {
		return fmt.Errorf("%w: administrative data must use canonical base64 or null", ErrManifestInvalid)
	}
	decodedBytes := base64.StdEncoding.DecodedLen(len(encoded))
	if strings.HasSuffix(encoded, "==") {
		decodedBytes -= 2
	} else if strings.HasSuffix(encoded, "=") {
		decodedBytes--
	}
	if decodedBytes > maximumAdministrativeBytes-budget.administrativeBytes {
		return manifestLimit("aggregate administrative bytes before typed decoding")
	}
	budget.administrativeBytes += decodedBytes
	return nil
}
