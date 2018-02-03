package plugin

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hashicorp/terraform/plugin/proto"
	"github.com/hashicorp/terraform/terraform"
	"google.golang.org/grpc"
)

// temporary functions for dealing with the old warning and errors slices
func diagnostics(warns []string, errs []error) []*proto.Diagnostic {
	diags := []*proto.Diagnostic{}
	for _, w := range warns {
		d := &proto.Diagnostic{
			Level:   proto.Diagnostic_WARNING,
			Summary: w,
		}
		diags = append(diags, d)
	}

	for _, e := range errs {
		d := &proto.Diagnostic{
			Level:   proto.Diagnostic_ERROR,
			Summary: e.Error(),
		}
		diags = append(diags, d)
	}

	return diags
}

func warnsAndErrs(diags []*proto.Diagnostic) ([]string, []error) {
	var warns []string
	var errs []error

	for _, d := range diags {
		switch d.Level {
		case proto.Diagnostic_ERROR:
			errs = append(errs, errors.New(d.Summary))
		case proto.Diagnostic_WARNING:
			warns = append(warns, d.Summary)
		}
	}

	return warns, errs
}

// dynamicValue encodes a terraform type into a proto.DynamicValue.
// Tetmporary function, using JSON for now.
func dynamicValue(i interface{}) *proto.DynamicValue {
	js, err := json.Marshal(i)
	if err != nil {
		panic(err)
	}

	return &proto.DynamicValue{Msgpack: js}
}

// terraform.ResourceProvider grpc implementation
type GRPCResourceProvider struct {
	conn   *grpc.ClientConn
	client proto.ProviderClient
}

func (p *GRPCResourceProvider) Stop() error {
	resp, err := p.client.Stop(context.TODO(), new(proto.Stop_Request))
	if err != nil {
		return err
	}

	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

func (p *GRPCResourceProvider) GetSchema(req *terraform.ProviderSchemaRequest) (*terraform.ProviderSchema, error) {
	resp, err := p.client.GetSchema(context.TODO(), &proto.GetSchema_Request{})
	if err != nil {
		return nil, err
	}

	var s terraform.ProviderSchema
	if err := json.Unmarshal(resp.ProviderSchema.Msgpack, &s); err != nil {
		panic(err)
	}

	return &s, nil
}

func (p *GRPCResourceProvider) Input(input terraform.UIInput, c *terraform.ResourceConfig) (*terraform.ResourceConfig, error) {
	return nil, errors.New("Not Implemented")
}

func (p *GRPCResourceProvider) Validate(c *terraform.ResourceConfig) ([]string, []error) {
	req := &proto.ValidateProviderConfig_Request{
		Config: dynamicValue(c),
	}
	resp, err := p.client.ValidateProviderConfig(context.TODO(), req)
	if err != nil {
		return nil, []error{err}
	}

	return warnsAndErrs(resp.Diagnostics)
}

func (p *GRPCResourceProvider) ValidateResource(t string, c *terraform.ResourceConfig) ([]string, []error) {
	req := &proto.ValidateResourceTypeConfig_Request{
		ResourceTypeName: t,
		Config:           dynamicValue(c),
	}

	resp, err := p.client.ValidateResourceTypeConfig(context.TODO(), req)
	if err != nil {
		return nil, []error{err}
	}

	return warnsAndErrs(resp.Diagnostics)
}

func (p *GRPCResourceProvider) ValidateDataSource(t string, c *terraform.ResourceConfig) ([]string, []error) {
	req := &proto.ValidateDataSourceConfig_Request{
		DataSourceName: t,
		Config:         dynamicValue(c),
	}

	resp, err := p.client.ValidateDataSourceConfig(context.TODO(), req)
	if err != nil {
		return nil, []error{err}
	}

	return warnsAndErrs(resp.Diagnostics)

}

func (p *GRPCResourceProvider) Configure(c *terraform.ResourceConfig) error {
	req := &proto.Configure_Request{
		Config: dynamicValue(c),
	}

	resp, err := p.client.Configure(context.TODO(), req)
	if err != nil {
		return err
	}

	_, errs := warnsAndErrs(resp.Diagnostics)
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (p *GRPCResourceProvider) Refresh(info *terraform.InstanceInfo, s *terraform.InstanceState) (*terraform.InstanceState, error) {
	args := struct {
		Info  *terraform.InstanceInfo
		State *terraform.InstanceState
	}{
		Info:  info,
		State: s,
	}

	req := &proto.ReadResource_Request{
		ResourceTypeName: info.Type,
		CurrentState:     dynamicValue(args),
	}

	resp, err := p.client.ReadResource(context.TODO(), req)
	if err != nil {
		return nil, err
	}

	newState := &terraform.InstanceState{}
	if err := json.Unmarshal(resp.NewState.Msgpack, newState); err != nil {
		return nil, err
	}

	return newState, nil
}

func (p *GRPCResourceProvider) Diff(info *terraform.InstanceInfo, s *terraform.InstanceState, c *terraform.ResourceConfig) (*terraform.InstanceDiff, error) {
	req := &proto.PlanResourceChange_Request{
		ResourceTypeName: info.Type,
		PriorState:       dynamicValue(s),
		ProposedNewState: dynamicValue(c),
	}

	resp, err := p.client.PlanResourceChange(context.TODO(), req)
	if err != nil {
		return nil, err
	}

	diff := &terraform.InstanceDiff{}
	if err := json.Unmarshal(resp.PlannedNewState.Msgpack, diff); err != nil {
		return nil, err
	}

	diags := proto.TFDiagnostics(resp.Diagnostics)
	return diff, diags.Err()
}

func (p *GRPCResourceProvider) Apply(info *terraform.InstanceInfo, s *terraform.InstanceState, d *terraform.InstanceDiff) (*terraform.InstanceState, error) {
	req := &proto.ApplyResourceChange_Request{
		ResourceTypeName: info.Type,
		PriorState:       dynamicValue(s),
		PlannedNewState:  dynamicValue(d),
	}

	resp, err := p.client.ApplyResourceChange(context.TODO(), req)
	if err != nil {
		return nil, err
	}

	state := &terraform.InstanceState{}
	if err := json.Unmarshal(resp.NewState.Msgpack, state); err != nil {
		return nil, err
	}

	diags := proto.TFDiagnostics(resp.Diagnostics)
	return state, diags.Err()
}

func (p *GRPCResourceProvider) ImportState(info *terraform.InstanceInfo, id string) ([]*terraform.InstanceState, error) {
	req := &proto.ImportResourceState_Request{
		ResourceTypeName: info.Type,
		Id:               id,
	}

	resp, err := p.client.ImportResourceState(context.TODO(), req)
	if err != nil {
		return nil, err
	}

	newState := []*terraform.InstanceState{}
	if err := json.Unmarshal(resp.NewState.Msgpack, &newState); err != nil {
		return nil, err
	}

	diags := proto.TFDiagnostics(resp.Diagnostics)
	return newState, diags.Err()
}

func (p *GRPCResourceProvider) Resources() []terraform.ResourceType {
	// delegated to the full schema
	s, err := p.GetSchema(nil)
	if err != nil {
		panic(err)
	}

	var resp []terraform.ResourceType
	for t, _ := range s.ResourceTypes {
		// FIXME: find Importable too
		resp = append(resp, terraform.ResourceType{Name: t, SchemaAvailable: true})
	}
	return resp
}

func (p *GRPCResourceProvider) ReadDataDiff(info *terraform.InstanceInfo, c *terraform.ResourceConfig) (*terraform.InstanceDiff, error) {
	req := &proto.ReadDataSource_Request{
		Request: dynamicValue([]interface{}{info, c}),
	}

	resp, err := p.client.TempDiffDataSource(context.TODO(), req)
	if err != nil {
		return nil, err
	}

	diff := &terraform.InstanceDiff{}
	if err := json.Unmarshal(resp.Result.Msgpack, diff); err != nil {
		return nil, err
	}

	diags := proto.TFDiagnostics(resp.Diagnostics)
	return diff, diags.Err()
}

func (p *GRPCResourceProvider) ReadDataApply(info *terraform.InstanceInfo, d *terraform.InstanceDiff) (*terraform.InstanceState, error) {
	req := &proto.ReadDataSource_Request{
		Request: dynamicValue(d),
	}

	resp, err := p.client.ReadDataSource(context.TODO(), req)
	if err != nil {
		return nil, err
	}

	state := &terraform.InstanceState{}
	if err := json.Unmarshal(resp.Result.Msgpack, state); err != nil {
		return nil, err
	}

	diags := proto.TFDiagnostics(resp.Diagnostics)
	return state, diags.Err()
}

func (p *GRPCResourceProvider) DataSources() []terraform.DataSource {
	// delegated to the full schema
	s, err := p.GetSchema(nil)
	if err != nil {
		panic(err)
	}

	var resp []terraform.DataSource
	for t, _ := range s.DataSources {
		// FIXME: find Importable too
		resp = append(resp, terraform.DataSource{Name: t, SchemaAvailable: true})
	}
	return resp
}

// closing the grpc connection is final, and terraform will call it at the end of every phase.
func (p *GRPCResourceProvider) Close() error {
	return nil
}

type GRPCResourceProviderServer struct {
	provider terraform.ResourceProvider
}

func (s *GRPCResourceProviderServer) GetSchema(_ context.Context, req *proto.GetSchema_Request) (*proto.GetSchema_Response, error) {
	// GetSchema must return the full schema
	ps, err := s.provider.GetSchema(nil)
	if err != nil {
		return nil, err
	}

	return &proto.GetSchema_Response{ProviderSchema: dynamicValue(ps)}, nil
}

func (s *GRPCResourceProviderServer) ValidateProviderConfig(_ context.Context, req *proto.ValidateProviderConfig_Request) (*proto.ValidateProviderConfig_Response, error) {
	rc := &terraform.ResourceConfig{}
	if err := json.Unmarshal(req.Config.Msgpack, rc); err != nil {
		return nil, err
	}

	warns, errs := s.provider.Validate(rc)

	return &proto.ValidateProviderConfig_Response{Diagnostics: diagnostics(warns, errs)}, nil
}

func (s *GRPCResourceProviderServer) ValidateResourceTypeConfig(_ context.Context, req *proto.ValidateResourceTypeConfig_Request) (*proto.ValidateResourceTypeConfig_Response, error) {

	cfg := &terraform.ResourceConfig{}
	if err := json.Unmarshal(req.Config.Msgpack, cfg); err != nil {
		return nil, err
	}

	w, e := s.provider.ValidateResource(req.ResourceTypeName, cfg)
	return &proto.ValidateResourceTypeConfig_Response{Diagnostics: diagnostics(w, e)}, nil
}

func (s *GRPCResourceProviderServer) Configure(_ context.Context, req *proto.Configure_Request) (*proto.Configure_Response, error) {

	cfg := &terraform.ResourceConfig{}
	if err := json.Unmarshal(req.Config.Msgpack, cfg); err != nil {
		return nil, err
	}

	err := s.provider.Configure(cfg)
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}

	return &proto.Configure_Response{Diagnostics: diagnostics(nil, errs)}, nil
}

func (s *GRPCResourceProviderServer) ReadResource(_ context.Context, req *proto.ReadResource_Request) (*proto.ReadResource_Response, error) {
	args := struct {
		Info  *terraform.InstanceInfo
		State *terraform.InstanceState
	}{}

	if err := json.Unmarshal(req.CurrentState.Msgpack, &args); err != nil {
		return nil, err
	}

	is, err := s.provider.Refresh(args.Info, args.State)
	if err != nil {
		return nil, err
	}

	return &proto.ReadResource_Response{NewState: dynamicValue(is)}, nil
}

func (s *GRPCResourceProviderServer) PlanResourceChange(_ context.Context, req *proto.PlanResourceChange_Request) (*proto.PlanResourceChange_Response, error) {
	info := &terraform.InstanceInfo{}
	state := &terraform.InstanceState{}
	cfg := &terraform.ResourceConfig{}

	if err := json.Unmarshal(req.PriorState.Msgpack, state); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(req.ProposedNewState.Msgpack, cfg); err != nil {
		return nil, err
	}

	info.Type = req.ResourceTypeName

	d, err := s.provider.Diff(info, state, cfg)
	if err != nil {
		return nil, err
	}

	return &proto.PlanResourceChange_Response{PlannedNewState: dynamicValue(d)}, nil
}

func (s *GRPCResourceProviderServer) ApplyResourceChange(_ context.Context, req *proto.ApplyResourceChange_Request) (*proto.ApplyResourceChange_Response, error) {

	info := &terraform.InstanceInfo{}
	state := &terraform.InstanceState{}
	diff := &terraform.InstanceDiff{}

	if err := json.Unmarshal(req.PriorState.Msgpack, state); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(req.PlannedNewState.Msgpack, diff); err != nil {
		return nil, err
	}

	info.Type = req.ResourceTypeName

	is, err := s.provider.Apply(info, state, diff)
	if err != nil {
		return nil, err
	}

	return &proto.ApplyResourceChange_Response{NewState: dynamicValue(is)}, nil
}

func (s *GRPCResourceProviderServer) ImportResourceState(_ context.Context, req *proto.ImportResourceState_Request) (*proto.ImportResourceState_Response, error) {
	info := &terraform.InstanceInfo{}
	info.Type = req.ResourceTypeName

	states, err := s.provider.ImportState(info, req.Id)
	if err != nil {
		return nil, err
	}

	return &proto.ImportResourceState_Response{NewState: dynamicValue(states)}, nil
}

func (s *GRPCResourceProviderServer) ValidateDataSourceConfig(_ context.Context, req *proto.ValidateDataSourceConfig_Request) (*proto.ValidateDataSourceConfig_Response, error) {
	cfg := &terraform.ResourceConfig{}
	if err := json.Unmarshal(req.Config.Msgpack, cfg); err != nil {
		return nil, err
	}

	w, e := s.provider.ValidateDataSource(req.DataSourceName, cfg)
	return &proto.ValidateDataSourceConfig_Response{Diagnostics: diagnostics(w, e)}, nil
}

func (s *GRPCResourceProviderServer) TempDiffDataSource(_ context.Context, req *proto.ReadDataSource_Request) (*proto.ReadDataSource_Response, error) {
	info := &terraform.InstanceInfo{}
	info.Type = req.DataSourceName

	cfg := &terraform.ResourceConfig{}
	if err := json.Unmarshal(req.Request.Msgpack, cfg); err != nil {
		return nil, err
	}

	diff, err := s.provider.ReadDataDiff(info, cfg)
	if err != nil {
		return nil, err
	}

	return &proto.ReadDataSource_Response{Result: dynamicValue(diff)}, nil
}

func (s *GRPCResourceProviderServer) ReadDataSource(_ context.Context, req *proto.ReadDataSource_Request) (*proto.ReadDataSource_Response, error) {
	info := &terraform.InstanceInfo{}
	info.Type = req.DataSourceName

	diff := &terraform.InstanceDiff{}
	if err := json.Unmarshal(req.Request.Msgpack, diff); err != nil {
		return nil, err
	}

	state, err := s.provider.ReadDataApply(info, diff)
	if err != nil {
		return nil, err
	}

	return &proto.ReadDataSource_Response{Result: dynamicValue(state)}, nil
}

func (s *GRPCResourceProviderServer) UpgradeResourceState(_ context.Context, _ *proto.UpgradeResourceState_Request) (*proto.UpgradeResourceState_Response, error) {
	return &proto.UpgradeResourceState_Response{}, nil
}

func (s *GRPCResourceProviderServer) Stop(_ context.Context, _ *proto.Stop_Request) (*proto.Stop_Response, error) {
	resp := &proto.Stop_Response{}

	err := s.provider.Stop()
	if err != nil {
		resp.Error = err.Error()
	}

	return resp, nil
}
