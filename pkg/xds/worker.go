package xds

import (
	"context"
	"fmt"
	"time"

	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/protobuf/types/known/anypb"

	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
)

type Worker struct {
	serverv3.CallbackFuncs

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
	}
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
		CommonHttpProtocolOptions: &corev3.HttpProtocolOptions{},
		Http2ProtocolOptions:      &corev3.Http2ProtocolOptions{},
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
				ConfigSource: &corev3.ConfigSource{
					ResourceApiVersion:    corev3.ApiVersion_V3,
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{},
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
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address:  "0.0.0.0",
					Protocol: corev3.SocketAddress_TCP,
					PortSpecifier: &corev3.SocketAddress_PortValue{
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
