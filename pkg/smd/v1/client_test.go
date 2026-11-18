package smd

import (
	"context"
	"crypto/tls"
	"errors"
	"math"
	"net"
	"sync/atomic"
	"testing"
	"time"

	legacy "github.com/rbln-sw/rblnlib-go/internal/proto/smdlegacy"
	pb "github.com/rbln-sw/rblnlib-go/internal/proto/smdv1"
	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

type inventoryServer struct {
	pb.UnimplementedInventoryServiceServer
	list func(context.Context, *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error)
}

func (s *inventoryServer) ListDevices(ctx context.Context, r *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
	return s.list(ctx, r)
}

type controlServer struct {
	pb.UnimplementedControlServiceServer
	enable func(context.Context, *pb.EnableVfsRequest) (*pb.EnableVfsResponse, error)
}

func (s *controlServer) EnableVfs(ctx context.Context, r *pb.EnableVfsRequest) (*pb.EnableVfsResponse, error) {
	return s.enable(ctx, r)
}

type legacyServer struct {
	legacy.UnimplementedRBLNServicesServer
	listed atomic.Int32
}

func (s *legacyServer) GetDeviceList(_ *legacy.Empty, stream grpc.ServerStreamingServer[legacy.Device]) error {
	s.listed.Add(1)
	return stream.Send(&legacy.Device{Name: "rbln0", PciBusId: "0000:01:00.0", DevId: "1220", CardName: "RBLN-Test"})
}
func (s *legacyServer) GetTotalInfo(_ *legacy.Empty, stream grpc.ServerStreamingServer[legacy.DeviceInfo]) error {
	return stream.Send(&legacy.DeviceInfo{Name: "rbln0", DevStatus: legacy.DeviceStatus_BUSY, ErrStatus: legacy.Status_SUCCEED, Temperature: 42500, Watt: 1500000, TotalMem: 8, UsedMem: 2, Utilization: 25, PState: 0})
}

type metricsServer struct {
	pb.UnimplementedMetricServiceServer
	group       *pb.MetricGroup
	samples     []*pb.MetricSample
	unreachable []*pb.EntityStatus
	sampled     atomic.Int32
	created     atomic.Int32
}

func (s *metricsServer) ListMetricGroups(context.Context, *pb.ListMetricGroupsRequest) (*pb.ListMetricGroupsResponse, error) {
	return &pb.ListMetricGroupsResponse{Items: []*pb.MetricGroup{s.group}}, nil
}
func (s *metricsServer) SampleMetrics(context.Context, *pb.SampleMetricsRequest) (*pb.SampleMetricsResponse, error) {
	s.sampled.Add(1)
	return &pb.SampleMetricsResponse{Samples: s.samples, Unreachable: s.unreachable}, nil
}
func (s *metricsServer) CreateMetricGroup(context.Context, *pb.CreateMetricGroupRequest) (*pb.CreateMetricGroupResponse, error) {
	s.created.Add(1)
	return nil, status.Error(codes.PermissionDenied, "should not create during reads")
}

type topologyServer struct {
	pb.UnimplementedTopologyServiceServer
}

func (*topologyServer) ListTopology(context.Context, *pb.ListTopologyRequest) (*pb.ListTopologyResponse, error) {
	return &pb.ListTopologyResponse{}, nil
}

func clientFor(t *testing.T, opts Options, register func(*grpc.Server)) *Client {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	register(server)
	go func() { _ = server.Serve(listener) }()
	opts.Endpoint = "passthrough:///test"
	opts.QueryTimeout = time.Second
	opts.ContextDialer = func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(); server.Stop(); _ = listener.Close() })
	return c
}
func wireDevice() *pb.DeviceInfo {
	return &pb.DeviceInfo{Name: "rbln0", PciBusId: "0000:01:00.0", DevId: "1220", State: pb.DeviceState_DEVICE_STATE_READY, NpuId: system.Pointer(uint32(0)), Versions: &pb.VersionInfo{DrvVersion: "4.0.0"}, Memory: &pb.MemorySpec{TotalBytes: 8589934592}}
}
func oneDevice(context.Context, *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
	return &pb.ListDevicesResponse{Items: []*pb.DeviceInfo{wireDevice()}}, nil
}

func TestFallbackOnlyForUnsupportedInitialInventory(t *testing.T) {
	for _, code := range []codes.Code{codes.Unimplemented, codes.Unavailable, codes.PermissionDenied} {
		t.Run(code.String(), func(t *testing.T) {
			old := &legacyServer{}
			c := clientFor(t, Options{}, func(s *grpc.Server) {
				pb.RegisterInventoryServiceServer(s, &inventoryServer{list: func(context.Context, *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
					return nil, status.Error(code, "failure")
				}})
				legacy.RegisterRBLNServicesServer(s, old)
			})
			got, err := c.ListDevices(context.Background())
			if code == codes.Unimplemented {
				if err != nil || got.Source != "smd/v0" || len(got.Devices) != 1 || got.Devices[0].State != system.StateBusy {
					t.Fatalf("legacy mapping: %+v %v", got, err)
				}
			} else if err == nil || old.listed.Load() != 0 {
				t.Fatalf("unauthorized fallback: %+v %v calls=%d", got, err, old.listed.Load())
			}
		})
	}
	old := &legacyServer{}
	c := clientFor(t, Options{}, func(s *grpc.Server) {
		pb.RegisterInventoryServiceServer(s, &inventoryServer{list: func(_ context.Context, r *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
			if r.PageToken == "" {
				return &pb.ListDevicesResponse{NextPageToken: "next"}, nil
			}
			return nil, status.Error(codes.Unimplemented, "second page")
		}})
		legacy.RegisterRBLNServicesServer(s, old)
	})
	if _, err := c.ListDevices(context.Background()); err == nil || old.listed.Load() != 0 {
		t.Fatalf("mid-stream fallback: %v calls=%d", err, old.listed.Load())
	}
}

func TestPaginationAndPartialInventory(t *testing.T) {
	c := clientFor(t, Options{}, func(s *grpc.Server) {
		pb.RegisterInventoryServiceServer(s, &inventoryServer{list: func(_ context.Context, r *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
			if r.PageToken == "" {
				return &pb.ListDevicesResponse{Items: []*pb.DeviceInfo{wireDevice()}, NextPageToken: "page2"}, nil
			}
			return &pb.ListDevicesResponse{Unreachable: []*pb.EntityStatus{{Ref: "rbln1"}}}, nil
		}})
	})
	s, err := c.ListDevices(context.Background())
	if err != nil || len(s.Devices) != 1 || len(s.Issues) != 1 || !errors.Is(s.Err(), system.ErrUnavailable) {
		t.Fatalf("partial lost: %+v %v", s, err)
	}
	c = clientFor(t, Options{}, func(s *grpc.Server) {
		pb.RegisterInventoryServiceServer(s, &inventoryServer{list: func(context.Context, *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
			return &pb.ListDevicesResponse{NextPageToken: "repeat"}, nil
		}})
	})
	if _, err := c.ListDevices(context.Background()); !errors.Is(err, system.ErrInvalidData) {
		t.Fatalf("loop not rejected: %v", err)
	}
}

func TestUnknownFutureStateDoesNotBecomeLegacyReady(t *testing.T) {
	for _, raw := range []pb.DeviceState{0, 99} {
		c := clientFor(t, Options{}, func(s *grpc.Server) {
			pb.RegisterInventoryServiceServer(s, &inventoryServer{list: func(context.Context, *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
				d := wireDevice()
				d.State = raw
				return &pb.ListDevicesResponse{Items: []*pb.DeviceInfo{d}}, nil
			}})
			legacy.RegisterRBLNServicesServer(s, &legacyServer{})
		})
		s, err := c.ListDevices(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := system.StateUnknown
		if raw == 0 {
			want = system.StateBusy
		}
		if s.Devices[0].State != want {
			t.Fatalf("raw %d became %s", raw, s.Devices[0].State)
		}
	}
}

func TestCommandsReturnPartialAndIndeterminateResults(t *testing.T) {
	var calls atomic.Int32
	c := clientFor(t, Options{}, func(s *grpc.Server) {
		pb.RegisterControlServiceServer(s, &controlServer{enable: func(_ context.Context, r *pb.EnableVfsRequest) (*pb.EnableVfsResponse, error) {
			calls.Add(1)
			if r.NumVfs != 2 || len(r.GetTargets().GetDevices().GetDevices()) != 2 || r.GetIdempotencyKey() != "operation-1" {
				t.Errorf("wrong request: %+v", r)
			}
			return &pb.EnableVfsResponse{AuditId: "audit", Results: []*pb.EntityStatus{{Ref: "rbln0"}, {Ref: "rbln1", Error: &pb.ErrorDetail{Code: 7002, Category: pb.ErrorCategory_ERROR_CATEGORY_CONFIG, OsErrno: 1}}}}, nil
		}})
	})
	r, err := c.EnableVFs(context.Background(), []string{"rbln0", "rbln1"}, 2, system.CommandOptions{IdempotencyKey: "operation-1"})
	if !errors.Is(err, system.ErrPermissionDenied) || r.Indeterminate || len(r.Outcomes) != 2 || r.Outcomes[0].Err != nil || calls.Load() != 1 {
		t.Fatalf("partial: %+v %v calls=%d", r, err, calls.Load())
	}
	for _, result := range [][]*pb.EntityStatus{nil, {{Ref: "wrong"}}, {{Ref: "rbln0"}, {Ref: "rbln0"}}} {
		c := clientFor(t, Options{}, func(s *grpc.Server) {
			pb.RegisterControlServiceServer(s, &controlServer{enable: func(context.Context, *pb.EnableVfsRequest) (*pb.EnableVfsResponse, error) {
				return &pb.EnableVfsResponse{Results: result}, nil
			}})
		})
		r, err := c.EnableVFs(context.Background(), []string{"rbln0"}, 1, system.CommandOptions{})
		if !r.Indeterminate || !errors.Is(err, system.ErrIndeterminate) {
			t.Fatalf("bad result accepted: %+v %v", r, err)
		}
	}
}

func TestTimeoutNeverReplaysControl(t *testing.T) {
	var calls atomic.Int32
	c := clientFor(t, Options{}, func(s *grpc.Server) {
		pb.RegisterControlServiceServer(s, &controlServer{enable: func(ctx context.Context, _ *pb.EnableVfsRequest) (*pb.EnableVfsResponse, error) {
			calls.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		}})
	})
	r, err := c.EnableVFs(context.Background(), []string{"rbln0"}, 1, system.CommandOptions{Timeout: 50 * time.Millisecond})
	if !r.Indeterminate || !errors.Is(err, system.ErrIndeterminate) || calls.Load() != 1 {
		t.Fatalf("timeout: %+v %v calls=%d", r, err, calls.Load())
	}
	if _, err := c.EnableVFs(context.Background(), nil, 1, system.CommandOptions{}); !errors.Is(err, system.ErrInvalidArgument) || calls.Load() != 1 {
		t.Fatalf("empty targets sent: %v", err)
	}
}

func TestCommandDeadlineMatchesWireSeconds(t *testing.T) {
	ctx, cancel, seconds, err := commandContext(context.Background(), 2, system.CommandOptions{PerTargetTimeout: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	deadline, _ := ctx.Deadline()
	if seconds != 1 || time.Until(deadline) < 31*time.Second {
		t.Fatalf("wire/client deadline mismatch: seconds=%d remaining=%s", seconds, time.Until(deadline))
	}
	r, err := commandError("EnableVFs", status.Error(codes.ResourceExhausted, "server resource failure"))
	if !r.Indeterminate || !errors.Is(err, system.ErrIndeterminate) {
		t.Fatalf("unknown resource failure treated as no effect: %+v %v", r, err)
	}
}

func TestVersionSpecificPermissionQuirk(t *testing.T) {
	for _, version := range []string{"", "3.5.1", "4.0.0"} {
		c := &Client{opts: Options{ServerVersion: version}}
		_, err := c.outcomes("EnableVFs", "", []string{"rbln0"}, []*pb.EntityStatus{{Ref: "rbln0", Error: &pb.ErrorDetail{Code: 7002, Category: pb.ErrorCategory_ERROR_CATEGORY_CONFIG, OsErrno: 1}}})
		want := system.ErrPermissionDenied
		if version == "3.5.1" {
			want = system.ErrNotReady
		}
		if !errors.Is(err, want) {
			t.Fatalf("version %q: %v", version, err)
		}
		if version == "3.5.1" && errors.Is(err, system.ErrPermissionDenied) {
			t.Fatalf("normalized transient still classified as denied: %v", err)
		}
	}
}

func TestTLSConfigurationDoesNotDowngrade(t *testing.T) {
	if cfg, err := LoadTLSConfig(TLSFiles{CertFile: "missing"}); cfg != nil || !errors.Is(err, system.ErrInvalidArgument) {
		t.Fatalf("incomplete identity: %+v %v", cfg, err)
	}
	if cfg, err := LoadTLSConfig(TLSFiles{CAFile: "/does-not-exist"}); cfg != nil || !errors.Is(err, system.ErrNotFound) {
		t.Fatalf("missing trust: %+v %v", cfg, err)
	}
	cfg, err := LoadTLSConfig(TLSFiles{})
	if err != nil || cfg == nil || cfg.MinVersion < tls.VersionTLS12 {
		t.Fatalf("system trust: %+v %v", cfg, err)
	}
}

func telemetryGroup() *pb.MetricGroup {
	return &pb.MetricGroup{Id: "test-group", MetricIds: append([]pb.MetricId(nil), telemetryIDs...), DeviceScope: &pb.DeviceScope{Selector: &pb.DeviceScope_All{All: &emptypb.Empty{}}}, SamplingPolicy: &pb.SamplingPolicy{Mode: &pb.SamplingPolicy_OnDemand{OnDemand: &emptypb.Empty{}}}}
}

func TestTelemetryUnitsAndReadOnlyCollection(t *testing.T) {
	server := &metricsServer{group: telemetryGroup(), samples: []*pb.MetricSample{
		{Device: &pb.DeviceRef{Name: "rbln0"}, MetricId: pb.MetricId_METRIC_ID_TEMPERATURE_MILLI_CELSIUS, TypedValue: &pb.MetricSample_FloatValue{FloatValue: 42500}},
		{Device: &pb.DeviceRef{Name: "rbln0"}, MetricId: pb.MetricId_METRIC_ID_POWER_MICRO_WATT, TypedValue: &pb.MetricSample_FloatValue{FloatValue: 1500000}},
		{Device: &pb.DeviceRef{Name: "rbln0"}, MetricId: pb.MetricId_METRIC_ID_UTILIZATION_PERCENT_MILLI, TypedValue: &pb.MetricSample_FloatValue{FloatValue: 12500}},
		{Device: &pb.DeviceRef{Name: "rbln0"}, MetricId: pb.MetricId_METRIC_ID_MEMORY_USED_BYTES, TypedValue: &pb.MetricSample_IntValue{IntValue: (1 << 53) + 1}},
	}}
	c := clientFor(t, Options{}, func(s *grpc.Server) {
		pb.RegisterInventoryServiceServer(s, &inventoryServer{list: oneDevice})
		pb.RegisterMetricServiceServer(s, server)
		pb.RegisterTopologyServiceServer(s, &topologyServer{})
	})
	s, err := c.SampleTelemetry(context.Background(), "test-group")
	if err != nil {
		t.Fatal(err)
	}
	v := s.Devices[0].Values
	if *v[system.TemperatureCelsius].Float != 42.5 || *v[system.PowerWatts].Float != 1.5 || *v[system.UtilizationPercent].Float != 12.5 || *v[system.MemoryUsedBytes].Uint != (1<<53)+1 {
		t.Fatalf("units: %+v", v)
	}
	if server.created.Load() != 0 || server.sampled.Load() != 1 {
		t.Fatal("query created configuration")
	}
	server.group.MetricIds = append(server.group.MetricIds, pb.MetricId_METRIC_ID_CLOCK_CP_0_MHZ)
	if _, err := c.SampleTelemetry(context.Background(), "test-group"); !errors.Is(err, system.ErrConflict) || server.sampled.Load() != 1 {
		t.Fatalf("sampled clock-bearing group: %v", err)
	}
}

func TestInvalidSamplesRemainUnavailable(t *testing.T) {
	for _, sample := range []*pb.MetricSample{
		{MetricId: pb.MetricId_METRIC_ID_POWER_MICRO_WATT, TypedValue: &pb.MetricSample_FloatValue{FloatValue: math.NaN()}},
		{MetricId: pb.MetricId_METRIC_ID_MEMORY_USED_BYTES, TypedValue: &pb.MetricSample_FloatValue{FloatValue: 5}},
		{MetricId: pb.MetricId_METRIC_ID_UTILIZATION_PERCENT_MILLI, TypedValue: &pb.MetricSample_FloatValue{FloatValue: 100001}},
	} {
		_, v, err := normalizedSample(sample)
		if !errors.Is(err, system.ErrInvalidData) || v.Float != nil || v.Uint != nil {
			t.Fatalf("invalid became reading: %+v %v", v, err)
		}
	}
	if legacyBytes(float32(math.NaN())) != nil || *legacyBytes(2) != 2*(1<<30) {
		t.Fatal("legacy GiB conversion")
	}
}

func TestLegacyTelemetryKeepsItsEnumAndUnits(t *testing.T) {
	c := clientFor(t, Options{}, func(s *grpc.Server) { legacy.RegisterRBLNServicesServer(s, &legacyServer{}) })
	s, err := c.SampleTelemetry(context.Background(), "unused-on-legacy")
	if err != nil || len(s.Devices) != 1 {
		t.Fatalf("legacy telemetry %+v %v", s, err)
	}
	d := s.Devices[0]
	if d.Device.State != system.StateBusy || *d.Values[system.PowerWatts].Float != 1.5 || *d.Values[system.MemoryUsedBytes].Uint != 2*(1<<30) {
		t.Fatalf("legacy normalization: %+v", d)
	}
}

func TestMissingReadingsRespectDeviceScope(t *testing.T) {
	for _, tc := range []struct {
		name        string
		vf, compute bool
		want        []system.Metric
	}{
		{"compute PF", false, true, []system.Metric{system.TemperatureCelsius, system.PowerWatts, system.MemoryUsedBytes, system.UtilizationPercent}},
		{"hosting PF", false, false, []system.Metric{system.TemperatureCelsius, system.PowerWatts}},
		{"compute VF", true, true, []system.Metric{system.MemoryUsedBytes, system.UtilizationPercent}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := wireDevice()
			device.IsVf = tc.vf
			if !tc.compute {
				device.NpuId = nil
			}
			server := &metricsServer{group: telemetryGroup(), samples: []*pb.MetricSample{
				{Device: &pb.DeviceRef{Name: device.Name}, MetricId: pb.MetricId_METRIC_ID_PSTATE, TypedValue: &pb.MetricSample_IntValue{IntValue: 0}},
			}}
			c := clientFor(t, Options{}, func(s *grpc.Server) {
				pb.RegisterInventoryServiceServer(s, &inventoryServer{list: func(context.Context, *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
					return &pb.ListDevicesResponse{Items: []*pb.DeviceInfo{device}}, nil
				}})
				pb.RegisterTopologyServiceServer(s, &topologyServer{})
				pb.RegisterMetricServiceServer(s, server)
			})
			s, err := c.SampleTelemetry(context.Background(), "test-group")
			if err != nil || len(s.Devices) != 1 {
				t.Fatalf("sample: %+v, %v", s, err)
			}
			r := s.Devices[0]
			if len(r.Values) != 1 || r.Values[system.PerformanceState].Uint == nil || *r.Values[system.PerformanceState].Uint != 0 || len(r.Issues) != len(tc.want) {
				t.Fatalf("values=%v issues=%v", r.Values, r.Issues)
			}
			fields := map[string]bool{}
			for _, i := range r.Issues {
				if !errors.Is(i.Err, system.ErrNotReported) {
					t.Errorf("unexpected missing metric error: %v", i.Err)
				}
				fields[i.Field] = true
				if i.Target != device.Name {
					t.Errorf("issue target %q", i.Target)
				}
			}
			for _, m := range tc.want {
				if !fields[string(m)] {
					t.Errorf("missing issue for %s", m)
				}
			}
		})
	}
}

func TestEmptyTelemetryResponseReportsAllExpectedReadings(t *testing.T) {
	c := clientFor(t, Options{}, func(s *grpc.Server) {
		pb.RegisterInventoryServiceServer(s, &inventoryServer{list: oneDevice})
		pb.RegisterTopologyServiceServer(s, &topologyServer{})
		pb.RegisterMetricServiceServer(s, &metricsServer{group: telemetryGroup()})
	})
	s, err := c.SampleTelemetry(context.Background(), "test-group")
	if err != nil || len(s.Devices) != 1 {
		t.Fatalf("sample: %+v %v", s, err)
	}
	r := s.Devices[0]
	if len(r.Values) != 0 || len(r.Issues) != 5 {
		t.Fatalf("empty response: %+v", r)
	}
	for _, i := range r.Issues {
		if !errors.Is(i.Err, system.ErrNotReported) {
			t.Fatalf("unexpected issue: %+v", i)
		}
	}
}
