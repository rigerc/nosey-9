// Package contracts validates Nosey's versioned data and machine-output contracts.
package contracts

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas/*.json
var files embed.FS
var cache sync.Map

type boundedPattern struct{ re *regexp2.Regexp }

func (p boundedPattern) String() string { return p.re.String() }
func (p boundedPattern) MatchString(value string) bool {
	match, err := p.re.MatchString(value)
	return err == nil && match
}

// Validate checks serialized JSON without allowing schemas to load remote resources.
func Validate(name string, data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	loaded, ok := cache.Load(name)
	if !ok {
		bytes, err := files.ReadFile("schemas/" + name)
		if err != nil {
			return err
		}
		var doc any
		if err = json.Unmarshal(bytes, &doc); err != nil {
			return err
		}
		c := jsonschema.NewCompiler()
		c.AssertFormat()
		c.UseRegexpEngine(func(pattern string) (jsonschema.Regexp, error) {
			re, compileErr := regexp2.Compile(pattern, regexp2.ECMAScript)
			if compileErr != nil {
				return nil, compileErr
			}
			re.MatchTimeout = 100 * time.Millisecond
			return boundedPattern{re}, nil
		})
		location := "https://nosey.invalid/schemas/" + name
		if err = c.AddResource(location, doc); err != nil {
			return err
		}
		schema, err := c.Compile(location)
		if err != nil {
			return err
		}
		loaded, _ = cache.LoadOrStore(name, schema)
	}
	schema, ok := loaded.(*jsonschema.Schema)
	if !ok {
		return fmt.Errorf("invalid cached schema %s", name)
	}
	err := schema.Validate(value)
	if err == nil {
		return nil
	}
	var validation *jsonschema.ValidationError
	if !errors.As(err, &validation) {
		return fmt.Errorf("%s validation failed", name)
	}
	locations := []string{}
	var collect func(*jsonschema.ValidationError)
	collect = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			locations = append(locations, "/"+strings.Join(e.InstanceLocation, "/")+" ("+strings.Join(e.ErrorKind.KeywordPath(), "/")+")")
		}
		for _, cause := range e.Causes {
			collect(cause)
		}
	}
	collect(validation)
	slices.Sort(locations)
	locations = slices.Compact(locations)
	if len(locations) > 12 {
		locations = locations[:12]
	}
	return fmt.Errorf("%s validation failed at %s", name, strings.Join(locations, ", "))
}

// Encode serializes an explicit output DTO and checks its published schema.
// encoding/json.Marshal defines the dynamic value accepted at this wire boundary.
func Encode[T any](name string, value T) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = Validate(name, data); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return append(data, '\n'), nil
}
