package gengateway

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/golang/glog"
	"github.com/grpc-ecosystem/grpc-gateway/utilities"
	"github.com/thesoulless/protoc-gen-gokitmux/descriptor"
	"github.com/thesoulless/protoc-gen-gokitmux/internal/casing"
)

type param struct {
	*descriptor.File
	Imports            []descriptor.GoPackage
	UseRequestContext  bool
	RegisterFuncSuffix string
	AllowPatchFeature  bool
	OutputPath         string
	Metrics            string
	ErrorEncoder       string
	PackageName        string
}

type params struct {
	Files              []*descriptor.File
	Imports            []descriptor.GoPackage
	UseRequestContext  bool
	RegisterFuncSuffix string
	AllowPatchFeature  bool
	OutputPath         string
	Metrics            string
	PackageName        string
}

type binding struct {
	*descriptor.Binding
	Registry          *descriptor.Registry
	AllowPatchFeature bool
	Ed string
}

// GetBodyFieldPath returns the binding body's fieldpath.
func (b binding) GetBodyFieldPath() string {
	if b.Body != nil && len(b.Body.FieldPath) != 0 {
		return b.Body.FieldPath.String()
	}
	return "*"
}

// GetBodyFieldPath returns the binding body's struct field name.
func (b binding) GetBodyFieldStructName() (string, error) {
	if b.Body != nil && len(b.Body.FieldPath) != 0 {
		return casing.Camel(b.Body.FieldPath.String()), nil
	}
	return "", errors.New("No body field found")
}

// HasQueryParam determines if the binding needs parameters in query string.
//
// It sometimes returns true even though actually the binding does not need.
// But it is not serious because it just results in a small amount of extra codes generated.
func (b binding) HasQueryParam() bool {
	if b.Body != nil && len(b.Body.FieldPath) == 0 {
		return false
	}
	fields := make(map[string]bool)
	for _, f := range b.Method.RequestType.Fields {
		fields[f.GetName()] = true
	}
	if b.Body != nil {
		delete(fields, b.Body.FieldPath.String())
	}
	for _, p := range b.PathParams {
		delete(fields, p.FieldPath.String())
	}
	return len(fields) > 0
}

func (b binding) QueryParamFilter() queryParamFilter {
	var seqs [][]string
	if b.Body != nil {
		seqs = append(seqs, strings.Split(b.Body.FieldPath.String(), "."))
	}
	for _, p := range b.PathParams {
		seqs = append(seqs, strings.Split(p.FieldPath.String(), "."))
	}
	return queryParamFilter{utilities.NewDoubleArray(seqs)}
}

// HasEnumPathParam returns true if the path parameter slice contains a parameter
// that maps to an enum proto field that is not repeated, if not false is returned.
func (b binding) HasEnumPathParam() bool {
	return b.hasEnumPathParam(false)
}

// HasRepeatedEnumPathParam returns true if the path parameter slice contains a parameter
// that maps to a repeated enum proto field, if not false is returned.
func (b binding) HasRepeatedEnumPathParam() bool {
	return b.hasEnumPathParam(true)
}

// hasEnumPathParam returns true if the path parameter slice contains a parameter
// that maps to a enum proto field and that the enum proto field is or isn't repeated
// based on the provided 'repeated' parameter.
func (b binding) hasEnumPathParam(repeated bool) bool {
	for _, p := range b.PathParams {
		if p.IsEnum() && p.IsRepeated() == repeated {
			return true
		}
	}
	return false
}

// LookupEnum looks up a enum type by path parameter.
func (b binding) LookupEnum(p descriptor.Parameter) *descriptor.Enum {
	e, err := b.Registry.LookupEnum("", p.Target.GetTypeName())
	if err != nil {
		return nil
	}
	return e
}

// FieldMaskField returns the golang-style name of the variable for a FieldMask, if there is exactly one of that type in
// the message. Otherwise, it returns an empty string.
func (b binding) FieldMaskField() string {
	var fieldMaskField *descriptor.Field
	for _, f := range b.Method.RequestType.Fields {
		if f.GetTypeName() == ".google.protobuf.FieldMask" {
			// if there is more than 1 FieldMask for this request, then return none
			if fieldMaskField != nil {
				return ""
			}
			fieldMaskField = f
		}
	}
	if fieldMaskField != nil {
		return casing.Camel(fieldMaskField.GetName())
	}
	return ""
}

// queryParamFilter is a wrapper of utilities.DoubleArray which provides String() to output DoubleArray.Encoding in a stable and predictable format.
type queryParamFilter struct {
	*utilities.DoubleArray
}

func (f queryParamFilter) String() string {
	encodings := make([]string, len(f.Encoding))
	for str, enc := range f.Encoding {
		encodings[enc] = fmt.Sprintf("%q: %d", str, enc)
	}
	e := strings.Join(encodings, ", ")
	return fmt.Sprintf("&utilities.DoubleArray{Encoding: map[string]int{%s}, Base: %#v, Check: %#v}", e, f.Base, f.Check)
}

type trailerParams struct {
	Files              []*descriptor.File
	Services           []*descriptor.Service
	UseRequestContext  bool
	RegisterFuncSuffix string
	AssumeColonVerb    bool
	ErrorEncoder       string
	PackageName        string
}

func applyTemplate(p param, reg *descriptor.Registry) (string, error) {
	w := bytes.NewBuffer(nil)
	p.Imports = []descriptor.GoPackage{}
	p.Imports = append(p.Imports, descriptor.GoPackage{
		Path:  "github.com/go-kit/kit/transport/http",
		Name:  "http",
		Alias: "httptransport",
	})
	if p.Metrics != "" {
		p.Imports = append(p.Imports, descriptor.GoPackage{
			Path: p.Metrics,
		})
	}
	//package {{.GoPkg.Name}}

	if err := kitHeaderTemplate.Execute(w, p); err != nil {
		return "", err
	}
	var targetServices []*descriptor.Service

	for _, msg := range p.Messages {
		msgName := casing.Camel(*msg.Name)
		msg.Name = &msgName
	}
	for _, svc := range p.Services {
		var methodWithBindingsSeen bool
		svcName := casing.Camel(*svc.Name)
		svc.Name = &svcName
		for _, meth := range svc.Methods {
			glog.V(2).Infof("Processing %s.%s", svc.GetName(), meth.GetName())
			methName := casing.Camel(*meth.Name)
			meth.Name = &methName
			for _, _ = range meth.Bindings {
				methodWithBindingsSeen = true
			}
		}
		if methodWithBindingsSeen {
			targetServices = append(targetServices, svc)
		}
	}
	if len(targetServices) == 0 {
		return "", errNoTargetService
	}

	assumeColonVerb := true
	if reg != nil {
		assumeColonVerb = !reg.GetAllowColonFinalSegments()
	}
	tp := trailerParams{
		Services:           targetServices,
		UseRequestContext:  p.UseRequestContext,
		RegisterFuncSuffix: p.RegisterFuncSuffix,
		AssumeColonVerb:    assumeColonVerb,
		ErrorEncoder:       p.ErrorEncoder,
		PackageName:        p.PackageName,
	}
	if err := kitTemplate.Execute(w, tp); err != nil {
			return "", err
		}
	return w.String(), nil
}

func applyServiceTemplate(ps params, reg *descriptor.Registry) (string, error) {
	w := bytes.NewBuffer(nil)
	ps.Imports = []descriptor.GoPackage{}
	moduleName := readModuleName()
	var targetServices []*descriptor.Service

	for _, p := range ps.Files {
		for _, msg := range p.Messages {
			msgName := casing.Camel(*msg.Name)
			msg.Name = &msgName
		}
	}

	for _, p := range ps.Files {
		for _, svc := range p.Services {
		var methodWithBindingsSeen bool
		svcName := casing.Camel(*svc.Name)
		svc.Name = &svcName
		fileName := *svc.File.Name
		packagePath := fileName[0:strings.LastIndex(*svc.File.Name, "/")]
		importName := moduleName + "/" + packagePath
		ps.Imports = append(ps.Imports, descriptor.GoPackage{
			Path: importName,
			Name: fileName[strings.LastIndex(*svc.File.Name, "/") : strings.LastIndex(*svc.File.Name, ".")-1],
		})
		for _, meth := range svc.Methods {
			glog.V(2).Infof("Processing %s.%s", svc.GetName(), meth.GetName())
			methName := casing.Camel(*meth.Name)
			meth.Name = &methName
			for _, _ = range meth.Bindings {
				methodWithBindingsSeen = true
			}
		}
		if methodWithBindingsSeen {
			targetServices = append(targetServices, svc)
		}
	}
	}

	if len(targetServices) == 0 {
		return "", errNoTargetService
	}
	ps.Imports = append(ps.Imports, descriptor.GoPackage{
		Path:  "context",
	})
	if err := serviceHeaderTemplate.Execute(w, ps); err != nil {
		return "", err
	}

	assumeColonVerb := true
	if reg != nil {
		assumeColonVerb = !reg.GetAllowColonFinalSegments()
	}
	tp := trailerParams{
		Files: ps.Files,
		Services:           targetServices,
		UseRequestContext:  ps.UseRequestContext,
		RegisterFuncSuffix: ps.RegisterFuncSuffix,
		AssumeColonVerb:    assumeColonVerb,
	}
	if err := serviceTemplate.Execute(w, tp); err != nil {
		return "", err
	}
	return w.String(), nil
}

func applyRoutesTemplate(ps params) (string, error) {
	w := bytes.NewBuffer(nil)
	ps.Imports = []descriptor.GoPackage{}
	ps.Imports = append(ps.Imports, descriptor.GoPackage{
		Path:  "github.com/gorilla/mux",
	})
	if err := serviceHeaderTemplate.Execute(w, ps); err != nil {
		return "", err
	}

	tp := trailerParams{
		Files: ps.Files,
		UseRequestContext:  ps.UseRequestContext,
		RegisterFuncSuffix: ps.RegisterFuncSuffix,
	}
	if err := routesTemplate.Execute(w, tp); err != nil {
		return "", err
	}
	return w.String(), nil
}

func applyEndpointsTemplate(ps params) (string, error) {
	w := bytes.NewBuffer(nil)
	ps.Imports = []descriptor.GoPackage{
		{
			Path: "context",
		},
		{
			Path: "github.com/go-kit/kit/endpoint",
		},
		{
			Path: "net/http",
		},
	}

	if err := serviceHeaderTemplate.Execute(w, ps); err != nil {
		return "", err
	}

	tp := trailerParams{
		//Files: ps.Files,
		UseRequestContext:  ps.UseRequestContext,
		RegisterFuncSuffix: ps.RegisterFuncSuffix,
	}
	if err := endpointsTemplate.Execute(w, tp); err != nil {
		return "", err
	}
	return w.String(), nil
}

func readModuleName() string {
	file, err := os.Open("go.mod")
	if err != nil {
		panic(err)
	}
	r := bufio.NewReader(file)
	line, _, err := r.ReadLine()
	if err != nil {
		panic(err)
	}
	moduleName := bytes.TrimPrefix(line, []byte("module "))
	return string(moduleName)
}

var (
	funcs     = template.FuncMap{"ToLower": strings.ToLower}

	kitHeaderTemplate = template.Must(template.New("header").Parse(`
// Code generated by protoc-gen-gokitmux. DO NOT EDIT.
// source: {{.GetName}}

/*
Package {{.PackageName}} is a reverse proxy.

It translates gRPC into RESTful JSON APIs.
*/
package {{.PackageName | printf "%s\n"}}

import (
	{{range $i := .Imports}}{{if $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}

	{{range $i := .Imports}}{{if not $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}
)
`))

	serviceHeaderTemplate = template.Must(template.New("header").Parse(`
// Code generated by protoc-gen-gokitmux. DO NOT EDIT.

package {{.PackageName | printf "%s\n"}}
import (
	{{range $i := .Imports}}{{if $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}

	{{range $i := .Imports}}{{if not $i.Standard}}{{$i | printf "%s\n"}}{{end}}{{end}}
)
`))

	kitTemplate = template.Must(template.New("kit").Funcs(funcs).Parse(`
{{$UseRequestContext := .UseRequestContext}}
{{$ErrorEncoder := .ErrorEncoder}}
func init() {
	{{range $i, $svc := .Services}}
	{{range $j, $m := $svc.Methods}}
	{{range $k, $b := $m.Bindings}}
	h{{$i}}{{$j}}{{$k}} := &{{$m.GetName}}{}
	RegisterHandler(h{{$i}}{{$j}}{{$k}})
	{{end}}
	{{end}}
	{{end}}
}
{{range $svc := .Services}}
	{{range $m := $svc.Methods}}
	{{range $b := $m.Bindings}}
	type {{$m.GetName}} struct{}
	{{end}}
	{{end}}
{{end}}
{{range $svc := .Services}}
	{{range $m := $svc.Methods}}
	{{range $b := $m.Bindings}}
	func (e *{{$m.GetName}}) Register(svc GatewayService) *Route {
		{{$m.GetName}}{{$.RegisterFuncSuffix}} := httptransport.NewServer(
			e.Make(svc),
			e.Decode,
			e.Encode,
			{{if $ErrorEncoder}}httptransport.ServerErrorEncoder({{$ErrorEncoder}}),{{end}}
		)
		{{$svc.GetName}}{{$.RegisterFuncSuffix}}Client := metrics.ForHandler(
			e.ForHandler({{$m.GetName}}{{$.RegisterFuncSuffix}}),
			"{{$m.GetName}}",
		)
		
		r := &Route{
			Path: {{$b.PathTmpl.Template | printf "%q"}},
			Handler: {{$svc.GetName}}{{$.RegisterFuncSuffix}}Client,
			Method: {{$b.HTTPMethod | printf "%q"}},
			{{with $n := $m.GetName }}Name: {{ ToLower $n | printf "%q"}},{{end}}
		}

		return r
	}
	{{end}}
	{{end}}
{{end}}`))

	serviceTemplate = template.Must(template.New("kit").Funcs(funcs).Parse(`
type GatewayService interface { {{range $f := .Files}}
{{range $svc := .Services}}// {{$svc.GetName}}
{{range $m := $svc.Methods}}{{range $b := $m.Bindings}}{{$m.GetName}}(context.Context, *{{$svc.File.Package}}.{{$b.Method.RequestType.Name}}) (*{{$svc.File.Package}}.{{$b.Method.ResponseType.Name}}, error)
{{end}}{{end}}
{{end}}
{{end}}
}`))

	routesTemplate = template.Must(template.New("kit").Funcs(funcs).Parse(`
func Router(svc GatewayService) *mux.Router {
	r := mux.NewRouter()

	for _, h := range Handlers {
		route := h.Register(svc)
		muxRoute := r.Handle(route.Path, route.Handler).Methods(route.Method)

		if route.Name != "" {
			muxRoute.Name(route.Name)
		}
	}

	r = ManualRouter(svc, r)

	return r
}`))

	endpointsTemplate = template.Must(template.New("kit").Funcs(funcs).Parse(`
type Endpointer interface {
	Register(GatewayService) *Route
	Make(GatewayService) endpoint.Endpoint
	Encode(context.Context, http.ResponseWriter, interface{}) error
	Decode(context.Context, *http.Request) (interface{}, error)
	ForHandler(handler http.Handler) http.Handler
}

type Route struct {
	Path    string
	Handler http.Handler
	Method string
	Name   string
}

var Handlers []Endpointer

func RegisterHandler(h Endpointer) {
	Handlers = append(Handlers, h)
}`))
)
