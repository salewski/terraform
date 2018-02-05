package plugin

import (
	"context"
	"errors"
	"io"

	"github.com/hashicorp/terraform/plugin/proto"
	"github.com/hashicorp/terraform/terraform"
	"google.golang.org/grpc"
)

// terraform.ResourceProvider grpc implementation
type GRPCResourceProvisioner struct {
	conn   *grpc.ClientConn
	client proto.ProvisionerClient
}

func (p *GRPCResourceProvisioner) Validate(c *terraform.ResourceConfig) ([]string, []error) {
	req := &proto.ValidateProvisionerConfig_Request{
		Config: dynamicValue(c),
	}
	resp, err := p.client.ValidateProvisionerConfig(context.TODO(), req)
	if err != nil {
		return nil, []error{err}
	}

	return warnsAndErrs(resp.Diagnostics)
}

func (p *GRPCResourceProvisioner) Apply(out terraform.UIOutput, s *terraform.InstanceState, c *terraform.ResourceConfig) error {
	payload := struct {
		State  *terraform.InstanceState
		Config *terraform.ResourceConfig
	}{
		State:  s,
		Config: c,
	}

	req := &proto.ProvisionerApply_Request{
		Config: dynamicValue(payload),
	}

	outputClient, err := p.client.Apply(context.TODO(), req)
	if err != nil {
		return err
	}

	for {
		resp, err := outputClient.Recv()
		if resp != nil {
			out.Output(resp.Output)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}

func (p *GRPCResourceProvisioner) Stop() error {
	resp, err := p.client.Stop(context.TODO(), &proto.Stop_Request{})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

func (p *GRPCResourceProvisioner) Close() error {
	return nil
}

type GRPCResourceProvisionerServer struct {
	provisioner terraform.ResourceProvisioner
}

func (s *GRPCResourceProvisionerServer) ValidateProvisionerConfig(_ context.Context, req *proto.ValidateProvisionerConfig_Request) (*proto.ValidateProvisionerConfig_Response, error) {
	cfg := &terraform.ResourceConfig{}
	unDynamicValue(req.Config, cfg)

	w, e := s.provisioner.Validate(cfg)
	return &proto.ValidateProvisionerConfig_Response{Diagnostics: diagnostics(w, e)}, nil
}

func (s *GRPCResourceProvisionerServer) Apply(req *proto.ProvisionerApply_Request, server proto.Provisioner_ApplyServer) error {
	payload := struct {
		State  *terraform.InstanceState
		Config *terraform.ResourceConfig
	}{}

	unDynamicValue(req.Config, &payload)

	return s.provisioner.Apply(&grpcOutputServer{server: server}, payload.State, payload.Config)
}

func (s *GRPCResourceProvisionerServer) Stop(_ context.Context, _ *proto.Stop_Request) (*proto.Stop_Response, error) {
	resp := &proto.Stop_Response{}
	err := s.provisioner.Stop()
	if err != nil {
		resp.Error = err.Error()
	}
	return resp, nil
}

type grpcOutputServer struct {
	server proto.Provisioner_ApplyServer
}

func (s *grpcOutputServer) Output(msg string) {
	s.server.Send(&proto.ProvisionerApply_Response{Output: msg})
}
