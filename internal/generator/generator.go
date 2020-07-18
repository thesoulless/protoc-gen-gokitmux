package generator

import (
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"github.com/thesoulless/protoc-gen-gokitmux/descriptor"
)

type Params struct {
	GenerateService bool
	MetricsPackage  string
	OutputPath      string
	ErrorEncoder    string
	PackageName     string
}

// Generator is an abstraction of code generators.
type Generator interface {
	// Generate generates output files from input .proto files.
	Generate(targets []*descriptor.File, p Params) ([]*plugin.CodeGeneratorResponse_File, error)
}
