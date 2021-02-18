package generator

import (
	"github.com/thesoulless/protoc-gen-gokitmux/descriptor"
	"google.golang.org/protobuf/types/pluginpb"
)

type Params struct {
	GenerateService    bool
	MetricsPackage     string
	ErrorEncoder       string
	PackageName        string
	RegisterFuncSuffix string
}

// Generator is an abstraction of code generators.
type Generator interface {
	Generate(targets []*descriptor.File, p Params) ([]*pluginpb.CodeGeneratorResponse_File, error)
}
