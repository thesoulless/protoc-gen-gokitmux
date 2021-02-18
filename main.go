package main

import (
	"flag"
	"fmt"
	"strings"

	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"

	"github.com/grpc-ecosystem/grpc-gateway/codegenerator"
	"github.com/thesoulless/protoc-gen-gokitmux/descriptor"

	"github.com/golang/glog"
	"github.com/thesoulless/protoc-gen-gokitmux/internal/generator"
	"github.com/thesoulless/protoc-gen-gokitmux/internal/gengateway"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"

	"os"

	"google.golang.org/protobuf/types/pluginpb"
)

var (
	importPrefix               = flag.String("import_prefix", "", "prefix to be added to go package paths for imported proto files")
	importPath                 = flag.String("import_path", "", "used as the package if no input files declare go_package. If it contains slashes, everything up to the rightmost slash is ignored.")
	registerFuncSuffix         = flag.String("register_func_suffix", "Handler", "used to construct names of generated Register*<Suffix> methods.")
	grpcAPIConfiguration       = flag.String("grpc_configuration", "", "path to gRPC API Configuration in YAML format")
	modulePath                 = flag.String("module", "", "specifies a module prefix that will be stripped from the go package to determine the output directory")
	repeatedPathParamSeparator = flag.String("repeated_path_param_separator", "csv", "configures how repeated fields should be split. Allowed values are `csv`, `pipes`, `ssv` and `tsv`.")
	metricsPackage             = flag.String("metrics", "", "path to metrics package")
	generateService            = flag.Bool("gen_service", false, "should a service interface be generated")
	errorEncoder               = flag.String("error_encoder", "", "sets error encoder name")
)

// Variables set by goreleaser at build time
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	flag.Parse()
	defer glog.Flush()
	reg := descriptor.NewRegistry()

	glog.V(1).Info("Parsing code generator request")
	req, err := codegenerator.ParseRequest(os.Stdin)
	if err != nil {
		glog.Fatal(err)
	}
	glog.V(1).Info("Parsed code generator request")

	protogen.Options{}.Run(func(gen *protogen.Plugin) error {

		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

		if req.Parameter != nil {
			for _, p := range strings.Split(req.GetParameter(), ",") {
				spec := strings.SplitN(p, "=", 2)
				if len(spec) == 1 {
					if err := flag.CommandLine.Set(spec[0], ""); err != nil {
						glog.Fatalf("Cannot set flag %s", p)
					}
					continue
				}
				name, value := spec[0], spec[1]
				if strings.HasPrefix(name, "M") {
					reg.AddPkgMap(name[1:], value)
					continue
				}
				if err := flag.CommandLine.Set(name, value); err != nil {
					glog.Fatalf("Cannot set flag %s", p)
				}
			}
		}

		if *grpcAPIConfiguration != "" {
			if err = reg.LoadGrpcAPIServiceFromYAML(*grpcAPIConfiguration); err != nil {
				emitError(err)
				panic(1)
			}
		}

		reg.SetPrefix(*importPrefix)
		reg.SetImportPath(*importPath)
		if err = reg.SetRepeatedPathParamSeparator(*repeatedPathParamSeparator); err != nil {
			emitError(err)
			panic(1)
		}
		if err = reg.Load(req); err != nil {
			emitError(err)
			panic(1)
		}
		unboundHTTPRules := reg.UnboundExternalHTTPRules()
		if len(unboundHTTPRules) != 0 {
			emitError(fmt.Errorf("HTTP rules without a matching selector: %s", strings.Join(unboundHTTPRules, ", ")))
			panic(1)
		}

		var targets []*descriptor.File
		for _, target := range req.FileToGenerate {
			f, _err := reg.LookupFile(target)
			if _err != nil {
				glog.Fatal(err)
			}
			targets = append(targets, f)
		}

		packageName := strings.Split(*modulePath, "/")
		PackageName := packageName[len(packageName)-1]

		ps := generator.Params{
			GenerateService:    *generateService,
			MetricsPackage:     *metricsPackage,
			ErrorEncoder:       *errorEncoder,
			PackageName:        PackageName,
			RegisterFuncSuffix: *registerFuncSuffix,
		}

		gwGen := gengateway.New(*modulePath)
		out, err := gwGen.Generate(targets, ps)
		glog.V(1).Info("Processed code generator request")
		if err != nil {
			emitError(err)
			return err
		}
		emitFiles(out)

		return nil
	})
}

func emitFiles(out []*plugin.CodeGeneratorResponse_File) {
	emitResp(&plugin.CodeGeneratorResponse{File: out})
}

func emitError(err error) {
	emitResp(&plugin.CodeGeneratorResponse{Error: proto.String(err.Error())})
}

func emitResp(resp *plugin.CodeGeneratorResponse) {
	buf, err := proto.Marshal(resp)
	if err != nil {
		glog.Fatal(err)
	}
	if _, err := os.Stdout.Write(buf); err != nil {
		glog.Fatal(err)
	}
}
