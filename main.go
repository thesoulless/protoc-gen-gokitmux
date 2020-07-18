package main

import (
	"flag"
	"fmt"
	gen "github.com/thesoulless/protoc-gen-gokitmux/internal/generator"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"github.com/grpc-ecosystem/grpc-gateway/codegenerator"
	"github.com/thesoulless/protoc-gen-gokitmux/descriptor"
	"github.com/thesoulless/protoc-gen-gokitmux/internal/gengateway"
)

var (
	importPrefix               = flag.String("import_prefix", "", "prefix to be added to go package paths for imported proto files")
	importPath                 = flag.String("import_path", "", "used as the package if no input files declare go_package. If it contains slashes, everything up to the rightmost slash is ignored.")
	registerFuncSuffix         = flag.String("register_func_suffix", "Handler", "used to construct names of generated Register*<Suffix> methods.")
	useRequestContext          = flag.Bool("request_context", true, "determine whether to use http.Request's context or not")
	allowDeleteBody            = flag.Bool("allow_delete_body", false, "unless set, HTTP DELETE methods may not have a body")
	grpcAPIConfiguration       = flag.String("grpc_configuration", "", "path to gRPC API Configuration in YAML format")
	pathType                   = flag.String("paths", "", "specifies how the paths of generated files are structured")
	modulePath                 = flag.String("module", "", "specifies a module prefix that will be stripped from the go package to determine the output directory")
	allowRepeatedFieldsInBody  = flag.Bool("allow_repeated_fields_in_body", false, "allows to use repeated field in `body` and `response_body` field of `google.api.http` annotation option")
	repeatedPathParamSeparator = flag.String("repeated_path_param_separator", "csv", "configures how repeated fields should be split. Allowed values are `csv`, `pipes`, `ssv` and `tsv`.")
	allowPatchFeature          = flag.Bool("allow_patch_feature", true, "determines whether to use PATCH feature involving update masks (using google.protobuf.FieldMask).")
	allowColonFinalSegments    = flag.Bool("allow_colon_final_segments", false, "determines whether colons are permitted in the final segment of a path")
	versionFlag                = flag.Bool("version", false, "print the current version")
	metricsPackage       	   = flag.String("metrics", "", "path to metrics package")
	generateService       	   = flag.Bool("gen_service", false, "should a service interface be generated")
	errorEncoder               = flag.String("error_encoder", "", "sets error encoder name")
	outPath                    = flag.String("out_path", "./endpoints", "output directory path")
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

	if *versionFlag {
		fmt.Printf("Version %v, commit %v, built at %v\n", version, commit, date)
		os.Exit(0)
	}

	reg := descriptor.NewRegistry()

	glog.V(1).Info("Parsing code generator request")
	req, err := codegenerator.ParseRequest(os.Stdin)
	if err != nil {
		glog.Fatal(err)
	}
	glog.V(1).Info("Parsed code generator request")

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

	g := gengateway.New(reg, *useRequestContext, *registerFuncSuffix, *pathType, *modulePath, *allowPatchFeature)

	if *grpcAPIConfiguration != "" {
		if err := reg.LoadGrpcAPIServiceFromYAML(*grpcAPIConfiguration); err != nil {
			emitError(err)
			return
		}
	}

	reg.SetPrefix(*importPrefix)
	reg.SetImportPath(*importPath)
	reg.SetAllowDeleteBody(*allowDeleteBody)
	reg.SetAllowRepeatedFieldsInBody(*allowRepeatedFieldsInBody)
	reg.SetAllowColonFinalSegments(*allowColonFinalSegments)
	if err := reg.SetRepeatedPathParamSeparator(*repeatedPathParamSeparator); err != nil {
		emitError(err)
		return
	}
	if err := reg.Load(req); err != nil {
		emitError(err)
		return
	}
	unboundHTTPRules := reg.UnboundExternalHTTPRules()
	if len(unboundHTTPRules) != 0 {
		emitError(fmt.Errorf("HTTP rules without a matching selector: %s", strings.Join(unboundHTTPRules, ", ")))
		return
	}

	var targets []*descriptor.File
	for _, target := range req.FileToGenerate {
		f, err := reg.LookupFile(target)
		if err != nil {
			glog.Fatal(err)
		}
		targets = append(targets, f)
	}

	p := gen.Params {
		GenerateService: *generateService,
		MetricsPackage:  *metricsPackage,
		OutputPath:      *outPath,
		ErrorEncoder:    *errorEncoder,
	}

	out, err := g.Generate(targets, p)
	glog.V(1).Info("Processed code generator request")
	if err != nil {
		emitError(err)
		return
	}
	emitFiles(out)
}

func parseEndpointDir(r io.Reader) (string, error) {
	input, _ := ioutil.ReadAll(r)
	glog.V(1).Infof("input %v", input)
	dir := "./endpoints"
	return dir, nil
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
