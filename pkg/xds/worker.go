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
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	tls_inspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	secret "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
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

	routeDetails map[string][]*route.Route // Keeping this separate to the route config

	updateInterval uint
	version        string
	dirty          bool // This is just because I don't want to track versions per resource yet.
}

func NewWorker(snapshotCache cache.SnapshotCache, updateInterval uint) *Worker {
	return &Worker{
		snapshotCache:  snapshotCache,
		updateInterval: updateInterval,

		endpoints: []types.Resource{},
		clusters:  []types.Resource{},
		routes:    []types.Resource{},
		listeners: []types.Resource{},
		secrets:   []types.Resource{},
		dirty:     true,

		routeDetails: map[string][]*route.Route{},

		CallbackFuncs: server.CallbackFuncs{
			DeltaStreamOpenFunc:     deltaStreamOpenFunc,
			DeltaStreamClosedFunc:   deltaStreamClosedFunc,
			StreamDeltaRequestFunc:  streamDeltaRequestFunc,
			StreamDeltaResponseFunc: streamDeltaResponseFunc,
			StreamOpenFunc:          streamOpenFunc,
			StreamClosedFunc:        streamClosedFunc,
			StreamRequestFunc:       streamRequestFunc,
			StreamResponseFunc:      streamResponseFunc,
		},
	}
}

func streamOpenFunc(ctx context.Context, i int64, s string) error {
	log.Infof("streamOpenFunc %d %s", i, s)
	return nil
}

func streamClosedFunc(i int64) {
	log.Infof("streamClosedFunc %d", i)
}

func streamRequestFunc(i int64, req *discovery.DiscoveryRequest) error {
	if req.ErrorDetail != nil {
		log.Errorf("%+v", req.ErrorDetail)
	}
	log.Infof("streamRequestFunc %d %s", i, req.TypeUrl)
	return nil
}

func streamResponseFunc(ctx context.Context, i int64, req *discovery.DiscoveryRequest, resp *discovery.DiscoveryResponse) {
	log.Infof("streamResponseFunc %d %s %d", i, req.TypeUrl, len(resp.Resources))
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

func (w *Worker) MarkDirty() {
	w.dirty = true
}

func (w *Worker) work(ctx context.Context) {
	if !w.dirty {
		// No changes posted so don't update
		return
	}
	w.version = fmt.Sprintf("%d", time.Now().UnixNano())
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

func (w *Worker) AddListener(name string, port uint32, secret string, servernames []string, routeConfigName string) {
	w.listeners = append(w.listeners, w.newListener(name, port, secret, servernames, routeConfigName))
}

func (w *Worker) DeleteListener(name string) {
	listeners := []types.Resource{}
	for _, resource := range w.listeners {
		lsnr := resource.(*listener.Listener)
		if lsnr.Name != name {
			listeners = append(listeners, lsnr)
		}
	}
	w.listeners = listeners
}

func (w *Worker) AddListenerFilterChain(name string, routeConfigName string, secret string, servernames []string) {
	for _, resource := range w.listeners {
		lsnr := resource.(*listener.Listener)
		if lsnr.Name == name {
			chain := w.newFilterChain(routeConfigName, secret, servernames)
			lsnr.FilterChains = append(lsnr.FilterChains, chain)
			break
		}
	}
}

func (w *Worker) DeleteListenerFilterChain(name string, routeConfigName string) {
	for _, resource := range w.listeners {
		lsnr := resource.(*listener.Listener)
		if lsnr.Name == name {
			chains := []*listener.FilterChain{}
			for _, chain := range lsnr.FilterChains {
				if chain.Name != routeConfigName {
					chains = append(chains, chain)
				}
			}
			lsnr.FilterChains = chains
			break
		}
	}
}

func (w *Worker) UpdateListenerFilterChain(listenerName string, routeConfigName string, secret string, servernames []string) {
	for _, resource := range w.listeners {
		lsnr := resource.(*listener.Listener)
		if lsnr.Name == listenerName {
			filterChains := []*listener.FilterChain{}

			for _, fc := range lsnr.GetFilterChains() {
				if fc.Name == routeConfigName {
					chain := w.newFilterChain(routeConfigName, secret, servernames)
					filterChains = append(filterChains, chain)
				} else {
					filterChains = append(filterChains, fc)
				}
			}
			lsnr.FilterChains = filterChains
			break
		}
	}
}

func (w *Worker) GetListenerNames() []string {
	names := []string{}
	for _, resource := range w.listeners {
		names = append(names, resource.(*listener.Listener).Name)
	}
	return names
}

func (w *Worker) newListener(name string, port uint32, secretName string, servernames []string, routeConfigName string) *listener.Listener {
	tlsInspectorConfig, _ := anypb.New(&tls_inspector.TlsInspector{})
	// Listener
	return &listener.Listener{
		Name: "amplify",
		Address: &core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address:  "0.0.0.0",
					Protocol: core.SocketAddress_TCP,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: uint32(port),
					},
				},
			},
		},
		FilterChains: []*listener.FilterChain{
			w.newFilterChain(routeConfigName, secretName, servernames),
		},
		ListenerFilters: []*listener.ListenerFilter{
			{
				Name:       "envoy.filters.listener.tls_inspector",
				ConfigType: &listener.ListenerFilter_TypedConfig{TypedConfig: tlsInspectorConfig},
			},
		},
	}
}

func (w *Worker) newFilterChain(routeCongName string, secretName string, servernames []string) *listener.FilterChain {
	routerFilterConfig, _ := anypb.New(&router.Router{})
	hcmConfig, err := anypb.New(&http_conn.HttpConnectionManager{
		CommonHttpProtocolOptions: &core.HttpProtocolOptions{},
		Http2ProtocolOptions:      &core.Http2ProtocolOptions{},
		InternalAddressConfig:     &http_conn.HttpConnectionManager_InternalAddressConfig{},
		StatPrefix:                routeCongName,
		StripMatchingHostPort:     true,
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
				RouteConfigName: routeCongName,
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

	filterChain := &listener.FilterChain{
		Filters: []*listener.Filter{
			{
				Name: "envoy.filters.network.http_connection_manager",
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: hcmConfig,
				},
			},
		},
	}

	if secretName != "" {
		downstreamTlsConfig, _ := anypb.New(&secret.DownstreamTlsContext{
			CommonTlsContext: &secret.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: []*secret.SdsSecretConfig{
					{
						Name: secretName,
						SdsConfig: &core.ConfigSource{
							ResourceApiVersion:    core.ApiVersion_V3,
							ConfigSourceSpecifier: &core.ConfigSource_Ads{},
						},
					},
				},
			},
		})

		filterChain.TransportSocket = &core.TransportSocket{
			Name: fmt.Sprintf("ts-%d", time.Now().Unix()),
			ConfigType: &core.TransportSocket_TypedConfig{
				TypedConfig: downstreamTlsConfig,
			},
		}
	}

	if servernames != nil {
		filterChain.FilterChainMatch = &listener.FilterChainMatch{
			ServerNames: servernames,
		}
	}

	return filterChain
}

func (w *Worker) GetClusterNames() []string {
	names := []string{}
	for _, resource := range w.clusters {
		names = append(names, resource.(*cluster.Cluster).Name)
	}
	return names
}

func (w *Worker) GetClusterDetails(name string) (string, uint32, bool) {
	for _, resource := range w.clusters {
		clst := resource.(*cluster.Cluster)
		if clst.Name == name {
			return clst.LoadAssignment.Endpoints[0].LbEndpoints[0].HostIdentifier.(*endpoint.LbEndpoint_Endpoint).Endpoint.Address.Address.(*core.Address_SocketAddress).SocketAddress.Address,
				clst.LoadAssignment.Endpoints[0].LbEndpoints[0].HostIdentifier.(*endpoint.LbEndpoint_Endpoint).Endpoint.Address.Address.(*core.Address_SocketAddress).SocketAddress.PortSpecifier.(*core.SocketAddress_PortValue).PortValue,
				(clst.TransportSocket != nil)
		}
	}
	return "", 0, false
}

func (w *Worker) GetRouteNames(routeConfigName string) []string {
	names := []string{}
	for _, route := range w.routeDetails[routeConfigName] {
		names = append(names, route.Name)
	}
	return names
}

func (w *Worker) GetRouteDetails(routeConfigName string, name string) (string, string) {
	for _, r := range w.routeDetails[routeConfigName] {
		if r.Name == name {
			return r.Match.GetPrefix(), r.Action.(*route.Route_Route).Route.ClusterSpecifier.(*route.RouteAction_Cluster).Cluster
		}
	}
	return "", ""
}

func (w *Worker) AddCluster(name string, host string, port uint32, tls bool, mtlsSecret string) {
	w.clusters = append(w.clusters, w.newCluster(name, host, port, tls, mtlsSecret))
}

func (w *Worker) UpdateCluster(name string, host string, port uint32, tls bool, mtlsSecret string) {
	clusters := []types.Resource{}

	for _, resource := range w.clusters {
		clst := resource.(*cluster.Cluster)
		if clst.Name == name {
			// Easier to recreate it
			clusters = append(clusters, w.newCluster(name, host, port, tls, mtlsSecret))
		} else {
			clusters = append(clusters, clst)
		}
	}
	w.clusters = clusters
}

func (w *Worker) DeleteCluster(name string) {
	clusters := []types.Resource{}
	for _, resource := range w.clusters {
		cluster := resource.(*cluster.Cluster)
		if cluster.Name != name {
			clusters = append(clusters, cluster)
		}
	}
	w.clusters = clusters
}

func (w *Worker) newCluster(name string, host string, port uint32, tls bool, mtlsSecretName string) *cluster.Cluster {
	var transportSocket *core.TransportSocket = nil

	if tls {
		mtlsSds := []*secret.SdsSecretConfig{}
		if mtlsSecretName != "" {
			mtlsSds = []*secret.SdsSecretConfig{
				{
					Name: mtlsSecretName,
					SdsConfig: &core.ConfigSource{
						ResourceApiVersion:    core.ApiVersion_V3,
						ConfigSourceSpecifier: &core.ConfigSource_Ads{},
					},
				},
			}
		}
		upstreamTlsConfig, _ := anypb.New(&secret.UpstreamTlsContext{
			CommonTlsContext: &secret.CommonTlsContext{
				TlsCertificateSdsSecretConfigs: mtlsSds,
			},
		})

		transportSocket = &core.TransportSocket{
			Name: name,
			ConfigType: &core.TransportSocket_TypedConfig{
				TypedConfig: upstreamTlsConfig,
			},
		}
	}

	return &cluster.Cluster{
		Name:              name,
		WaitForWarmOnInit: &wrapperspb.BoolValue{Value: true},
		ClusterDiscoveryType: &cluster.Cluster_Type{
			Type: cluster.Cluster_STRICT_DNS,
		},
		TransportSocket: transportSocket,
		DnsLookupFamily: cluster.Cluster_V4_ONLY,
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
												PortSpecifier: &core.SocketAddress_PortValue{PortValue: port},
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

func (w *Worker) AddRoute(routeConfigName string, name string, prefix string, cluster string) {
	if _, ok := w.routeDetails[routeConfigName]; !ok {
		w.routeDetails[routeConfigName] = []*route.Route{}
	}
	w.routeDetails[routeConfigName] = append(w.routeDetails[routeConfigName], w.newRoute(name, prefix, cluster))
	w.rebuildRoutes(routeConfigName)
}

func (w *Worker) UpdateRoute(routeConfigName string, name string, prefix string, cluster string) {
	for _, r := range w.routeDetails[routeConfigName] {
		if r.Name == name {
			r.Match.PathSpecifier.(*route.RouteMatch_Prefix).Prefix = prefix
			r.Action.(*route.Route_Route).Route.ClusterSpecifier.(*route.RouteAction_Cluster).Cluster = cluster
			break
		}
	}
	w.rebuildRoutes(routeConfigName)
}

func (w *Worker) DeleteRoute(routeConfigName string, name string) {
	routes := []*route.Route{}
	for _, r := range w.routeDetails[routeConfigName] {
		if r.Name != name {
			routes = append(routes, r)
		}
	}
	w.routeDetails[routeConfigName] = routes
	w.rebuildRoutes(routeConfigName)
}

func (w *Worker) AddRouteConfiguration(routeConfigName string, domains []string) {
	if _, ok := w.routeDetails[routeConfigName]; ok {
		log.Errorf("Route config %s already exists", routeConfigName)
		return
	}
	w.routeDetails[routeConfigName] = []*route.Route{}

	routecfg := &route.RouteConfiguration{
		Name:             routeConfigName,
		ValidateClusters: &wrapperspb.BoolValue{Value: true},
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    routeConfigName,
				Domains: domains,
				Routes:  w.routeDetails[routeConfigName],
			},
		},
	}
	w.routes = append(w.routes, routecfg)
}

func (w *Worker) UpdateRouteConfiguration(name string, domains []string) {
	for _, resource := range w.routes {
		rc := resource.(*route.RouteConfiguration)
		if rc.Name == name {
			rc.VirtualHosts[0].Domains = domains
			break
		}
	}
}

func (w *Worker) DeleteRouteConfiguration(name string) {
	routes := []types.Resource{}
	for _, resource := range w.routes {
		rc := resource.(*route.RouteConfiguration)
		if rc.Name != name {
			routes = append(routes, rc)
		}
	}
	w.routes = routes
}

func (w *Worker) GetRouteConfigurationNames() []string {
	names := []string{}
	for _, resource := range w.routes {
		rc := resource.(*route.RouteConfiguration)
		names = append(names, rc.Name)
	}
	return names
}

func (w *Worker) GetRouteConfigurationDetails(name string) []string {
	domains := []string{}
	for _, resource := range w.routes {
		rc := resource.(*route.RouteConfiguration)
		if rc.Name == name {
			domains = rc.VirtualHosts[0].Domains
			break
		}
	}
	return domains
}

func (w *Worker) rebuildRoutes(name string) {
	for _, resource := range w.routes {
		rc := resource.(*route.RouteConfiguration)
		if rc.Name == name {
			rc.VirtualHosts[0].Routes = w.routeDetails[name]
			break
		}
	}
}

func (w *Worker) newRoute(name string, prefix string, cluster string) *route.Route {
	return &route.Route{
		Name: name,
		Match: &route.RouteMatch{
			PathSpecifier: &route.RouteMatch_Prefix{
				Prefix: prefix,
			},
		},
		Action: &route.Route_Route{
			Route: &route.RouteAction{
				HostRewriteSpecifier: &route.RouteAction_AutoHostRewrite{AutoHostRewrite: &wrapperspb.BoolValue{Value: true}},
				ClusterSpecifier: &route.RouteAction_Cluster{
					Cluster: cluster,
				},
			},
		},
	}
}

func (w *Worker) AddSecret(name string, keyPath string, certPath string, password string) {
	w.secrets = append(w.secrets, w.newSecret(name, keyPath, certPath, password))
}

func (w *Worker) UpdateSecret(name string, keyPath string, certPath string, password string) {
	for _, resource := range w.secrets {
		sec := resource.(*secret.Secret)
		if sec.Name == name {
			sec.Type.(*secret.Secret_TlsCertificate).TlsCertificate = &secret.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: certPath,
					},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: keyPath,
					},
				},
				Password: &core.DataSource{
					Specifier: &core.DataSource_InlineString{
						InlineString: password,
					},
				},
			}
			break
		}
	}
}

func (w *Worker) DeleteSecret(name string) {
	secrets := []types.Resource{}
	for _, resource := range w.secrets {
		sec := resource.(*secret.Secret)
		if sec.Name != name {
			secrets = append(secrets, sec)
		}
	}
	w.secrets = secrets
}

func (w *Worker) GetSecretNames() []string {
	names := []string{}
	for _, sec := range w.secrets {
		names = append(names, sec.(*secret.Secret).Name)
	}
	return names
}

func (w *Worker) GetSecretDetails(name string) (string, string, string) {
	for _, resource := range w.secrets {
		sec := resource.(*secret.Secret)
		if sec.Name == name {
			return sec.GetTlsCertificate().GetPrivateKey().GetFilename(), sec.GetTlsCertificate().GetCertificateChain().GetFilename(), sec.GetTlsCertificate().Password.GetInlineString()
		}
	}
	return "", "", ""
}

func (w *Worker) newSecret(name string, keyPath string, certPath string, password string) *secret.Secret {
	return &secret.Secret{
		Name: name,
		Type: &secret.Secret_TlsCertificate{
			TlsCertificate: &secret.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: certPath,
					},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: keyPath,
					},
				},
				Password: &core.DataSource{
					Specifier: &core.DataSource_InlineString{
						InlineString: password,
					},
				},
			},
		},
	}
}
