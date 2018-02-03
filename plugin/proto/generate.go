//go:generate protoc -I ./ provider.proto provisioner.proto --go_out=plugins=grpc:./

package proto
