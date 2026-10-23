// Command genschema writes the JSON schema of the messaging contract from
// the Go types in pkg/proto, for clients not written in Go. It runs through
// go generate in pkg/proto.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/spytify/spytify/pkg/proto"
)

func main() {
	out := flag.String("o", "schema.json", "output file")
	flag.Parse()
	b, err := json.MarshalIndent(build(), "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type gen struct{ defs map[string]any }

func build() map[string]any {
	g := &gen{defs: map[string]any{}}
	methods := map[string]any{}
	for _, m := range proto.Methods {
		entry := map[string]any{"description": m.Doc, "result": g.schema(reflect.TypeOf(m.Result))}
		if m.Params != nil {
			entry["params"] = g.schema(reflect.TypeOf(m.Params))
		}
		methods[m.Name] = entry
	}
	events := map[string]any{}
	for _, e := range proto.Events {
		events[e.Name] = map[string]any{"description": e.Doc, "params": g.schema(reflect.TypeOf(e.Payload))}
	}
	return map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"title":       "spytify messaging contract",
		"description": "Newline-delimited JSON-RPC 2.0. Actions are requests, events are notifications.",
		"protocol":    proto.Protocol,
		"methods":     methods,
		"events":      events,
		"$defs":       g.defs,
	}
}

func (g *gen) schema(t reflect.Type) any {
	if t == nil {
		return map[string]any{}
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return g.schema(t.Elem())
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": g.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.schema(t.Elem())}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		name := t.Name()
		if _, ok := g.defs[name]; !ok {
			g.defs[name] = nil
			props := map[string]any{}
			var required []string
			g.fields(t, props, &required)
			sort.Strings(required)
			def := map[string]any{"type": "object", "properties": props}
			if len(required) > 0 {
				def["required"] = required
			}
			g.defs[name] = def
		}
		return map[string]any{"$ref": "#/$defs/" + name}
	}
	return map[string]any{}
}

func (g *gen) fields(t reflect.Type, props map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			g.fields(f.Type, props, required)
			continue
		}
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		props[name] = g.schema(f.Type)
		if !strings.Contains(opts, "omitempty") {
			*required = append(*required, name)
		}
	}
}
