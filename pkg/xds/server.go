package xds

import (
	"context"
	"fmt"
	"net"

	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"

	discoveryservice "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	// Loading these triggers the population of protoregistry via their inits.
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
)

// Run entry point for Envoy XDS command line.
func Run() error {

	snapshotCache := cache.NewSnapshotCache(true, cache.IDHash{}, log)
	worker := NewWorker(snapshotCache, 5000)

	server := serverv3.NewServer(context.Background(), snapshotCache, worker)
	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", config.Port))
	if err != nil {
		log.Fatal(err)
	}

	discoveryservice.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)

	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			log.Fatal(err)
		}
	}()

	worker.Start(context.Background())
	log.Infof("Listening on %d", config.Port)

	menuMain(worker)

	grpcServer.GracefulStop()
	log.Info("Shutdown")
	return nil
}
