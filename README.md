# protoc-gen-gokitmux
Protoc plugin to generate a Http gateway based on gokit and mux.

This plugin is based on [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway)'s generator, and assumes you are using Go Modules.

## Installation
`go install github.com/thesoulless/protoc-gen-gokitmux`

## Usage

### Commandline arguments
* `out_path` Outout directory od the generated files.
* `metrics` Metrics package. Generator will use ForHandler method of the package to track metrics of each route. (optional)
* `error_encoder` Gokit custom error encoder function. (optional)
* `gen_service` If plugin should negerate the service [interface] file. (optional)


### Sample Usage
```
protoc -I. --gokitmux_out=logtostderr=true,out_path=./gen,paths=source_relative,metrics=github.com/user/repo/metrics,error_encoder=myErrorEncoder,gen_service=true,grpc_configuration=pb/api.yaml:./ pb/hi.proto pb/bye.proto pb/other.proto;
```
