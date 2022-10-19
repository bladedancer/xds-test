package xds

import (
	"context"
	"fmt"
	"time"

	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	server "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
)

type Worker struct {
	server.CallbackFuncs

	snapshotCache cache.SnapshotCache

	endpoints []types.Resource
	clusters  []types.Resource
	routes    []types.Resource
	listeners []types.Resource
	secrets   []types.Resource

	updateInterval uint
	curPort        uint
	version        string
	dirty          bool // This is just because I don't want to track versions per resource yet.
}

func NewWorker(snapshotCache cache.SnapshotCache, updateInterval uint) *Worker {
	return &Worker{
		snapshotCache:  snapshotCache,
		updateInterval: updateInterval,
		curPort:        42420,

		endpoints: []types.Resource{},
		clusters:  []types.Resource{},
		routes:    []types.Resource{},
		listeners: []types.Resource{},
		secrets:   []types.Resource{},
		dirty:     true,

		CallbackFuncs: server.CallbackFuncs{
			DeltaStreamOpenFunc:     deltaStreamOpenFunc,
			DeltaStreamClosedFunc:   deltaStreamClosedFunc,
			StreamDeltaRequestFunc:  streamDeltaRequestFunc,
			StreamDeltaResponseFunc: streamDeltaResponseFunc,
		},
	}

}

func deltaStreamOpenFunc(ctx context.Context, i int64, s string) error {
	log.Infof("deltaStreamOpenFunc %d %s", i, s)
	return nil
}

func deltaStreamClosedFunc(i int64) {
	log.Infof("deltaStreamClosedFunc %d", i)
}

func streamDeltaRequestFunc(i int64, req *discovery.DeltaDiscoveryRequest) error {
	if req.ErrorDetail != nil {
		log.Errorf("%+v", req.ErrorDetail)
	}
	log.Infof("streamDeltaRequestFunc %d %s", i, req.TypeUrl)
	return nil
}

func streamDeltaResponseFunc(i int64, req *discovery.DeltaDiscoveryRequest, resp *discovery.DeltaDiscoveryResponse) {
	log.Infof("streamDeltaResponseFunc %d %s %d", i, req.TypeUrl, len(resp.Resources))
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Duration(w.updateInterval) * time.Millisecond)
		for {
			select {
			case <-ticker.C:
				w.work(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *Worker) work(ctx context.Context) {
	if !w.dirty {
		// No changes posted so don't update
		return
	}

	// Update the snapshot SEEMS DODGY PROBABLY SHOULD BE UPDATING RESOURCES
	snapshot, err := cache.NewSnapshot(w.version,
		map[string][]types.Resource{
			resource.EndpointType: w.endpoints,
			resource.ClusterType:  w.clusters,
			resource.RouteType:    w.routes,
			resource.ListenerType: w.listeners,
			resource.SecretType:   w.secrets,
		})

	if err != nil {
		log.Fatal(err)
	}

	err = w.snapshotCache.SetSnapshot(ctx, "xdstest", snapshot)
	if err != nil {
		log.Fatal(err)
	}
	w.dirty = false
}

func (w *Worker) UpdateListener() {
	w.version = fmt.Sprintf("%d", time.Now().UnixNano())

	w.curPort += 1
	w.listeners = []types.Resource{w.newListener()}
	w.dirty = true
}

func (w *Worker) newListener() *listener.Listener {
	routerFilterConfig, _ := anypb.New(&router.Router{})
	hcmConfig, err := anypb.New(&http_conn.HttpConnectionManager{
		CommonHttpProtocolOptions: &core.HttpProtocolOptions{},
		Http2ProtocolOptions:      &core.Http2ProtocolOptions{},
		InternalAddressConfig:     &http_conn.HttpConnectionManager_InternalAddressConfig{},
		StatPrefix:                "xdstest",
		HttpFilters: []*http_conn.HttpFilter{
			{
				Name: wellknown.Router,
				ConfigType: &http_conn.HttpFilter_TypedConfig{
					TypedConfig: routerFilterConfig,
				},
			},
		},
		RouteSpecifier: &http_conn.HttpConnectionManager_Rds{
			Rds: &http_conn.Rds{
				RouteConfigName: "xdstest",
				ConfigSource: &core.ConfigSource{
					ResourceApiVersion:    core.ApiVersion_V3,
					ConfigSourceSpecifier: &core.ConfigSource_Ads{},
				},
			},
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	// Listener
	return &listener.Listener{
		Name: "amplify",
		Address: &core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address:  "0.0.0.0",
					Protocol: core.SocketAddress_TCP,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: uint32(w.curPort),
					},
				},
			},
		},
		FilterChains: []*listener.FilterChain{
			{
				Filters: []*listener.Filter{
					{
						Name: "envoy.filters.network.http_connection_manager",
						ConfigType: &listener.Filter_TypedConfig{
							TypedConfig: hcmConfig,
						},
					},
				},
			},
		},
	}
}

func (w *Worker) GetClusterNames() []string {
	names := []string{}
	for _, resource := range w.clusters {
		names = append(names, resource.(*cluster.Cluster).Name)
	}
	return names
}

func (w *Worker) GetClusterHostname(name string) string {
	for _, resource := range w.clusters {
		clst := resource.(*cluster.Cluster)
		if clst.Name == name {
			return clst.LoadAssignment.Endpoints[0].LbEndpoints[0].HostIdentifier.(*endpoint.LbEndpoint_Endpoint).Endpoint.Address.Address.(*core.Address_SocketAddress).SocketAddress.Address
		}
	}
	return ""
}

func (w *Worker) AddCluster(name string, host string) {
	w.clusters = append(w.clusters, w.newCluster(name, host))
	w.dirty = true
}

func (w *Worker) UpdateCluster(name string, host string) {
	for _, resource := range w.clusters {
		clst := resource.(*cluster.Cluster)
		if clst.Name == name {
			clst.LoadAssignment.Endpoints[0].LbEndpoints[0].HostIdentifier.(*endpoint.LbEndpoint_Endpoint).Endpoint.Address.Address.(*core.Address_SocketAddress).SocketAddress.Address = host
			w.dirty = true
			break
		}
	}
}

func (w *Worker) newCluster(name string, host string) *cluster.Cluster {
	return &cluster.Cluster{
		Name:              name,
		WaitForWarmOnInit: &wrapperspb.BoolValue{Value: true},
		ClusterDiscoveryType: &cluster.Cluster_Type{
			Type: cluster.Cluster_STRICT_DNS,
		},
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpoint.LbEndpoint{
						{
							HostIdentifier: &endpoint.LbEndpoint_Endpoint{
								Endpoint: &endpoint.Endpoint{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address:       host,
												PortSpecifier: &core.SocketAddress_PortValue{PortValue: 8080},
												Protocol:      core.SocketAddress_TCP,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
