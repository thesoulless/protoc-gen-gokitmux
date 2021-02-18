package gengateway

import (
	"errors"
	"fmt"
	"go/format"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/thesoulless/protoc-gen-gokitmux/descriptor"
	gen "github.com/thesoulless/protoc-gen-gokitmux/internal/generator"
)

var (
	errNoTargetService = errors.New("no target service defined in the file")
)

type generator struct {
	modulePath string
}

// New returns a new generator which generates grpc gateway files.
func New(modulePath string) gen.Generator {
	return &generator{
		modulePath: modulePath,
	}
}

func (g *generator) Generate(targets []*descriptor.File, p gen.Params) ([]*plugin.CodeGeneratorResponse_File, error) {
	var files []*pluginpb.CodeGeneratorResponse_File

	// Services
	srvFiles, err := g.generateServices(targets, p)
	if err != nil {
		return nil, err
	}
	files = append(files, srvFiles...)

	// Router
	router, err := g.generateRouter(p)
	if err != nil {
		return nil, err
	}
	files = append(files, router)

	// Muxkit
	muxkit, err := g.generateMuxkit(targets, p)
	if err != nil {
		return nil, err
	}
	files = append(files, muxkit)

	// Endpoints
	endpoints, err := g.generateEndpoints(p)
	if err != nil {
		return nil, err
	}
	files = append(files, endpoints)

	return files, nil
}

func (g *generator) generateService(file *descriptor.File, p gen.Params) (string, error) {
	ps := param{
		File:         file,
		Metrics:      p.MetricsPackage,
		ErrorEncoder: p.ErrorEncoder,
		PackageName:  p.PackageName,
	}
	return applyTemplate(ps)
}

func (g *generator) generateServices(files []*descriptor.File, p gen.Params) ([]*plugin.CodeGeneratorResponse_File, error) {
	var outFiles []*plugin.CodeGeneratorResponse_File
	for _, f := range files {
		code, _err := g.generateService(f, p)
		if _err != nil {
			return nil, _err
		}
		formatted, err := format.Source([]byte(code))
		if err != nil {
			glog.Errorf("%v: %s", err, code)
			return nil, err
		}
		fmtStr := string(formatted)
		fileNames := strings.Split(f.GetName(), "/")
		name := fileNames[len(fileNames)-1]
		ext := filepath.Ext(name)
		name = strings.TrimSuffix(name, ext)
		base := name + ".gm"
		output := fmt.Sprintf("%s/%s/%s.go", g.modulePath, name, base)
		outFiles = append(outFiles, &plugin.CodeGeneratorResponse_File{
			Name:    &output,
			Content: &fmtStr,
		})
	}

	return outFiles, nil
}

func (g *generator) generateRouter(p gen.Params) (*plugin.CodeGeneratorResponse_File, error) {
	ps := params{
		Metrics:     p.MetricsPackage,
		PackageName: p.PackageName,
	}
	code, err := applyRoutesTemplate(ps)
	if err != nil {
		return nil, err
	}

	formatted, err := format.Source([]byte(code))
	fmtStr := string(formatted)
	if err != nil {
		glog.Errorf("%v: %s", err, code)
		panic(err)
	}
	base := g.modulePath + "/" + "routes.gm"
	output := fmt.Sprintf("%s.go", base)
	return &plugin.CodeGeneratorResponse_File{
		Name:    &output,
		Content: &fmtStr,
	}, nil
}

func (g *generator) generateMuxkit(files []*descriptor.File, p gen.Params) (*plugin.CodeGeneratorResponse_File, error) {
	params := params{
		Files:       files,
		Metrics:     p.MetricsPackage,
		PackageName: p.PackageName,
	}
	code, err := applyMuxkitTemplate(params)
	if err != nil {
		return nil, err
	}
	formatted, err := format.Source([]byte(code))
	if err != nil {
		return nil, err
	}
	fmtStr := string(formatted)
	base := fmt.Sprintf("%s/%s/%s", g.modulePath, "muxkit", "muxkit.gm")
	output := fmt.Sprintf("%s.go", base)
	return &plugin.CodeGeneratorResponse_File{
		Name:    &output,
		Content: &fmtStr,
	}, nil
}

func (g *generator) generateEndpoints(p gen.Params) (*plugin.CodeGeneratorResponse_File, error) {
	params := params{
		Metrics:     p.MetricsPackage,
		PackageName: p.PackageName,
	}
	code, err := applyEndpointsTemplate(params)
	if err != nil {
		return nil, err
	}
	formatted, err := format.Source([]byte(code))
	if err != nil {
		return nil, err
	}
	fmtStr := string(formatted)
	base := g.modulePath + "/" + "endpoints.gm"
	output := fmt.Sprintf("%s.go", base)
	return &plugin.CodeGeneratorResponse_File{
		Name:    &output,
		Content: &fmtStr,
	}, nil
}
