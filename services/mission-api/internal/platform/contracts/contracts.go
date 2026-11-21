// Package contracts loads the repository's published JSON Schemas and example
// payloads so the service can be verified against the same files the console
// is verified against. A change to one side then fails the other side's tests
// instead of drifting quietly.
package contracts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaID is the $id the published schemas use, so a schema can be addressed
// by name regardless of where it was read from on disk.
const schemaID = "https://sentinel-grid.local/schemas/"

// ErrNotFound reports that the contracts directory could not be located.
var ErrNotFound = errors.New("contracts directory not found")

// Dir walks up from the working directory until it finds contracts/.
func Dir() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for depth := 0; depth < 8; depth++ {
		// The published tree is recognised by its schemas directory: this
		// package's own folder is also called contracts, and matching on the
		// name alone would find that instead.
		candidate := filepath.Join(directory, "contracts")
		if info, err := os.Stat(filepath.Join(candidate, "schemas")); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}

	return "", ErrNotFound
}

func load(path string) (any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer file.Close()

	document, err := jsonschema.UnmarshalJSON(file)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return document, nil
}

// Compile compiles the named schema. Names of schemas it references through
// $ref are passed as also, because every resource has to be registered before
// compilation.
func Compile(name string, also ...string) (*jsonschema.Schema, error) {
	directory, err := Dir()
	if err != nil {
		return nil, err
	}

	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()

	for _, schemaName := range append([]string{name}, also...) {
		document, err := load(filepath.Join(directory, "schemas", schemaName+".schema.json"))
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource(schemaID+schemaName+".schema.json", document); err != nil {
			return nil, fmt.Errorf("add %s: %w", schemaName, err)
		}
	}

	schema, err := compiler.Compile(schemaID + name + ".schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", name, err)
	}
	return schema, nil
}

// Example reads a published example payload by name.
func Example(name string) (any, error) {
	directory, err := Dir()
	if err != nil {
		return nil, err
	}
	return load(filepath.Join(directory, "examples", name+".json"))
}
